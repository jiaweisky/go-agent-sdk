package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// setBaseURL points the AMap client at a stub server for the test.
func setBaseURL(t *testing.T, server *httptest.Server) {
	t.Helper()
	old := amapBaseURL
	if server != nil {
		amapBaseURL = server.URL
	}
	t.Cleanup(func() { amapBaseURL = old })
}

// newAmapStub serves the two AMap endpoints from fixed JSON bodies and
// redirects the client at it. A body for an endpoint a test never reaches may
// be empty.
func newAmapStub(t *testing.T, geocode, weather string) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/geocode/geo":
			_, _ = w.Write([]byte(geocode))
		case "/v3/weather/weatherInfo":
			_, _ = w.Write([]byte(weather))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	setBaseURL(t, server)
	return server.Client()
}

func TestFormatLive(t *testing.T) {
	body := json.RawMessage(`{"status":"1","lives":[{"province":"北京市","city":"东城区","weather":"晴","temperature":"18","winddirection":"南","windpower":"≤3","humidity":"52","reporttime":"2026-10-19 12:00:00"}]}`)
	got, err := formatLive(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"北京市 东城区", "Weather: 晴", "Temperature: 18°C", "Wind power: ≤3", "Humidity: 52%"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatLive missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatLiveEmpty(t *testing.T) {
	got, err := formatLive(json.RawMessage(`{"status":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "no live weather data" {
		t.Errorf("formatLive empty = %q", got)
	}
}

func TestFormatForecast(t *testing.T) {
	body := json.RawMessage(`{"status":"1","forecasts":[{"province":"北京市","city":"东城区","casts":[
		{"date":"2026-10-19","week":"1","dayweather":"晴","nightweather":"多云","daytemp":"20","nighttemp":"10","daywind":"南","daypower":"3","nightwind":"北","nightpower":"≤3"},
		{"date":"2026-10-20","week":"2","dayweather":"阴","nightweather":"小雨","daytemp":"18","nighttemp":"9","daywind":"东","daypower":"≤3","nightwind":"东","nightpower":"≤3"}]}]}`)
	got, err := formatForecast(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"北京市 东城区", "Today (1)", "2026-10-20 (2)", "Day: 晴 | Night: 多云", "Temperature: 10°C ~ 20°C"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatForecast missing %q in:\n%s", want, got)
		}
	}
}

func TestWeatherToolCurrent(t *testing.T) {
	client := newAmapStub(t,
		`{"status":"1","geocodes":[{"adcode":"110101","citycode":"010"}]}`,
		`{"status":"1","lives":[{"province":"北京市","city":"东城区","weather":"晴","temperature":"18","winddirection":"南","windpower":"≤3","humidity":"52","reporttime":"2026-10-19 12:00:00"}]}`)
	res, _, err := weatherTool(context.Background(), "test-key", client, weatherInput{CityName: "北京市"})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, res)
	for _, want := range []string{"北京市 东城区", "Weather: 晴", "Query date"} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q:\n%s", want, text)
		}
	}
}

func TestWeatherToolForecastExtension(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/weather/weatherInfo" {
			gotQuery = r.URL.Query().Get("extensions")
			_, _ = w.Write([]byte(`{"status":"1","forecasts":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"1","geocodes":[{"adcode":"110101"}]}`))
	}))
	t.Cleanup(server.Close)
	setBaseURL(t, server)

	res, _, err := weatherTool(context.Background(), "test-key", server.Client(),
		weatherInput{CityName: "北京市", ForecastType: "forecast"})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "all" {
		t.Errorf("extensions query = %q, want all", gotQuery)
	}
	if got := resultText(t, res); !strings.Contains(got, "no forecast weather data") {
		t.Errorf("forecast text = %q", got)
	}
}

func TestWeatherToolErrors(t *testing.T) {
	cases := []struct {
		name string
		key  string
		in   weatherInput
		want string
	}{
		{"missing key", "  ", weatherInput{CityName: "北京市"}, "AMAP_API_KEY is not configured"},
		{"empty city", "k", weatherInput{CityName: " "}, "please provide a valid city name"},
		{"bad type", "k", weatherInput{CityName: "北京市", ForecastType: "weekly"}, "must be current or forecast"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := weatherTool(context.Background(), tc.key, http.DefaultClient, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := resultText(t, res); !strings.Contains(got, tc.want) {
				t.Errorf("text = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestWeatherToolUnknownCity(t *testing.T) {
	client := newAmapStub(t, `{"status":"1","geocodes":[]}`, "")
	res, _, err := weatherTool(context.Background(), "k", client, weatherInput{CityName: "不存在市"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, res); !strings.Contains(got, "cannot resolve city") {
		t.Errorf("text = %q", got)
	}
}

func TestWeatherToolAmapError(t *testing.T) {
	client := newAmapStub(t, `{"status":"1","geocodes":[{"adcode":"110101"}]}`, `{"status":"0","info":"INVALID_USER_KEY"}`)
	res, _, err := weatherTool(context.Background(), "k", client, weatherInput{CityName: "北京市"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, res); !strings.Contains(got, "INVALID_USER_KEY") {
		t.Errorf("text = %q", got)
	}
}

func TestGeocodeFallsBackToCitycode(t *testing.T) {
	client := newAmapStub(t, `{"status":"1","geocodes":[{"adcode":"","citycode":"010"}]}`, "")
	code, err := geocode(context.Background(), client, "k", "北京市")
	if err != nil {
		t.Fatal(err)
	}
	if code != "010" {
		t.Errorf("geocode = %q, want 010", code)
	}
}

// resultText joins the text content of a tool result.
func resultText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
