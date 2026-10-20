// Command weathermcp is a small MCP server exposing AMap weather queries.
// It speaks MCP over stdio and backs the chat example's local (stdio) MCP
// support: the SDK spawns and owns this process, unlike the notes server,
// which chat launches itself over streamable HTTP.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// amapBaseURL is the root of the AMap REST API. It is a var so tests
// can point it at a stub server.
var amapBaseURL = "https://restapi.amap.com"

func main() {
	server := newServer(os.Getenv("AMAP_API_KEY"), http.DefaultClient)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, &sdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "weathermcp:", err)
		os.Exit(1)
	}
}

// newServer builds the MCP server around the given AMap key and HTTP client,
// so tests can inject both.
func newServer(key string, client *http.Client) *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{
			Name:        "weather",
			Version:     "0.1.0",
			Description: "Weather queries for the go-agent-sdk chat example.",
		},
		&sdk.ServerOptions{Instructions: "Query live or forecast weather for Chinese cities via the AMap REST API. " +
			"Use get-weather-forecast with a city name; set forecastType to \"forecast\" for a multi-day outlook."},
	)
	registerTools(server, key, client)
	return server
}

// weatherInput is the get-weather-forecast tool input. forecastType accepts
// "current" (default) or "forecast"; unknown values are reported in the
// result text.
type weatherInput struct {
	CityName     string `json:"cityName" jsonschema:"Chinese prefecture-level city name, e.g. 北京市, 上海市, 广州市"`
	Date         string `json:"date,omitempty" jsonschema:"query date, YYYY-MM-DD (optional, defaults to today; only labels the result)"`
	ForecastType string `json:"forecastType,omitempty" jsonschema:"query type: current (live weather) or forecast (multi-day outlook), defaults to current"`
}

// registerTools exposes the AMap queries as the get-weather-forecast tool.
// Business problems (missing key, unknown city, API errors) are returned as
// result text rather than transport errors, so the model can react to them.
func registerTools(server *sdk.Server, key string, client *http.Client) {
	sdk.AddTool[weatherInput, any](server, &sdk.Tool{
		Name:        "get-weather-forecast",
		Description: "Query live or forecast weather for Chinese cities (AMap REST API).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in weatherInput) (*sdk.CallToolResult, any, error) {
		return weatherTool(ctx, key, client, in)
	})
}

// weatherTool implements the get-weather-forecast handler.
func weatherTool(ctx context.Context, key string, client *http.Client, in weatherInput) (*sdk.CallToolResult, any, error) {
	cityName, forecastType, fail := validateWeatherInput(key, in)
	if fail != nil {
		return fail, nil, nil
	}
	formatted, err := queryWeather(ctx, key, client, cityName, forecastType)
	if err != nil {
		return textResult("weather query failed: %v", err), nil, nil
	}
	date := in.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return textResult("Weather query result\n\n%s\n\nQuery date: %s", formatted, date), nil, nil
}

// validateWeatherInput normalizes and checks the tool input, returning the
// trimmed city name and the query type, or a ready-to-return error result.
func validateWeatherInput(key string, in weatherInput) (cityName, forecastType string, fail *sdk.CallToolResult) {
	if strings.TrimSpace(key) == "" {
		// AMAP_API_KEY must be a Web Service key registered at https://lbs.amap.com/
		// (Console -> Application Management -> Create Application; enable the
		// Web Service API for it).
		return "", "", textResult("error: AMAP_API_KEY is not configured; set it in the environment")
	}
	cityName = strings.TrimSpace(in.CityName)
	if cityName == "" {
		return "", "", textResult("error: please provide a valid city name")
	}
	forecastType = in.ForecastType
	if forecastType == "" {
		forecastType = "current"
	}
	if forecastType != "current" && forecastType != "forecast" {
		return "", "", textResult("error: forecastType must be current or forecast")
	}
	return cityName, forecastType, nil
}

// queryWeather resolves the city and returns the formatted live or forecast
// text for the requested query type.
func queryWeather(ctx context.Context, key string, client *http.Client, cityName, forecastType string) (string, error) {
	fmt.Fprintf(os.Stderr, "geocoding city: %s\n", cityName)
	code, err := geocode(ctx, client, key, cityName)
	if err != nil {
		return "", err
	}
	if code == "" {
		return "", fmt.Errorf("cannot resolve city %q; check the name", cityName)
	}
	fmt.Fprintf(os.Stderr, "city code resolved: %s\n", code)

	fmt.Fprintf(os.Stderr, "querying weather, type: %s\n", forecastType)
	extensions := "base"
	if forecastType == "forecast" {
		extensions = "all"
	}
	body, err := weatherInfo(ctx, client, key, code, extensions)
	if err != nil {
		return "", err
	}
	if forecastType == "forecast" {
		return formatForecast(body)
	}
	return formatLive(body)
}

// geocode resolves a city name to its adcode (falling back to citycode),
// returning "" when the API answers OK but without a match.
func geocode(ctx context.Context, client *http.Client, key, address string) (string, error) {
	var out struct {
		Status   string `json:"status"`
		Info     string `json:"info"`
		Geocodes []struct {
			Adcode   string `json:"adcode"`
			Citycode string `json:"citycode"`
		} `json:"geocodes"`
	}
	body, err := amapFetch(ctx, client, key, "/v3/geocode/geo",
		map[string]string{"address": address})
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Status != "1" {
		return "", fmt.Errorf("geocode failed: %s", out.Info)
	}
	if len(out.Geocodes) == 0 {
		return "", nil
	}
	if out.Geocodes[0].Adcode != "" {
		return out.Geocodes[0].Adcode, nil
	}
	return out.Geocodes[0].Citycode, nil
}

// weatherInfo fetches live (extensions=base) or forecast (extensions=all)
// weather for a city code and returns the raw JSON body.
func weatherInfo(ctx context.Context, client *http.Client, key, cityCode, extensions string) (json.RawMessage, error) {
	body, err := amapFetch(ctx, client, key, "/v3/weather/weatherInfo",
		map[string]string{"city": cityCode, "extensions": extensions})
	if err != nil {
		return nil, err
	}
	var status struct {
		Status string `json:"status"`
		Info   string `json:"info"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	if status.Status != "1" {
		return nil, fmt.Errorf("weather query failed: %s", status.Info)
	}
	return body, nil
}

// amapFetch calls one AMap REST endpoint with the shared parameters and
// returns the raw JSON response.
func amapFetch(ctx context.Context, client *http.Client, key, endpoint string, params map[string]string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("key", key)
	q.Set("output", "json")
	for k, v := range params {
		q.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, amapBaseURL+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// textResult renders a plain-text tool result.
func textResult(format string, args ...any) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

// formatLive renders a base (live) weather body as text.
func formatLive(body json.RawMessage) (string, error) {
	var out struct {
		Lives []struct {
			Province      string `json:"province"`
			City          string `json:"city"`
			Weather       string `json:"weather"`
			Temperature   string `json:"temperature"`
			Winddirection string `json:"winddirection"`
			Windpower     string `json:"windpower"`
			Humidity      string `json:"humidity"`
			Reporttime    string `json:"reporttime"`
		} `json:"lives"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Lives) == 0 {
		return "no live weather data", nil
	}
	live := out.Lives[0]
	return fmt.Sprintf("📍 %s %s\n🌤️  Weather: %s\n🌡️  Temperature: %s°C\n💨 Wind: %s\n🌪️  Wind power: %s\n💧 Humidity: %s%%\n⏰ Updated: %s",
		live.Province, live.City, live.Weather, live.Temperature,
		live.Winddirection, live.Windpower, live.Humidity, live.Reporttime), nil
}

// formatForecast renders an all (forecast) weather body as text.
func formatForecast(body json.RawMessage) (string, error) {
	var out struct {
		Forecasts []struct {
			Province string `json:"province"`
			City     string `json:"city"`
			Casts    []struct {
				Date         string `json:"date"`
				Week         string `json:"week"`
				Dayweather   string `json:"dayweather"`
				Nightweather string `json:"nightweather"`
				Daytemp      string `json:"daytemp"`
				Nighttemp    string `json:"nighttemp"`
				Daywind      string `json:"daywind"`
				Daypower     string `json:"daypower"`
				Nightwind    string `json:"nightwind"`
				Nightpower   string `json:"nightpower"`
			} `json:"casts"`
		} `json:"forecasts"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Forecasts) == 0 {
		return "no forecast weather data", nil
	}
	forecast := out.Forecasts[0]
	var b strings.Builder
	fmt.Fprintf(&b, "📍 %s %s\n\n📅 Multi-day forecast:\n", forecast.Province, forecast.City)
	for i, cast := range forecast.Casts {
		day := cast.Date
		if i == 0 {
			day = "Today"
		}
		fmt.Fprintf(&b, "\n%s (%s)\n", day, cast.Week)
		fmt.Fprintf(&b, "🌤️  Day: %s | Night: %s\n", cast.Dayweather, cast.Nightweather)
		fmt.Fprintf(&b, "🌡️  Temperature: %s°C ~ %s°C\n", cast.Nighttemp, cast.Daytemp)
		fmt.Fprintf(&b, "💨 Wind: %s %s | %s %s\n", cast.Daywind, cast.Daypower, cast.Nightwind, cast.Nightpower)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
