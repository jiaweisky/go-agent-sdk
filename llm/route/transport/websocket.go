package transport

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

type WebSocketRequest struct {
	URL     string
	Headers map[string]string
}

type WebSocketConnection struct {
	SendText func(ctx context.Context, message string) error
	Messages func() iter.Seq2[any, error]
	Close    func() error
}

func messageText(msg any) string {
	switch v := msg.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func webSocketURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("Invalid WebSocket URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("Unsupported WebSocket URL protocol %s", u.Scheme)
	}
	return u.String(), nil
}

func OpenWebSocket(ctx context.Context, input WebSocketRequest) (*WebSocketConnection, error) {
	wsURL, err := webSocketURL(input.URL)
	if err != nil {
		return nil, &schema.LLMError{
			Module: "WebSocketExecutor",
			Method: "open",
			Reason: schema.NewTransportReason(err.Error(), schema.WithURL(input.URL)),
		}
	}
	header := make(map[string][]string)
	for k, v := range input.Headers {
		header[k] = []string{v}
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, &schema.LLMError{
			Module: "WebSocketExecutor",
			Method: "open",
			Reason: schema.NewTransportReason(fmt.Sprintf("Failed to open WebSocket: %v", err), schema.WithURL(input.URL)),
		}
	}
	done := make(chan struct{})
	msgCh := make(chan any, 128)

	go func() {
		defer close(done)
		defer close(msgCh)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					msgCh <- &schema.LLMError{
						Module: "WebSocketExecutor",
						Method: "message",
						Reason: schema.NewTransportReason(fmt.Sprintf("WebSocket error: %v", err), schema.WithURL(input.URL)),
					}
				}
				return
			}
			select {
			case msgCh <- message:
			case <-done:
				return
			}
		}
	}()

	return &WebSocketConnection{
		SendText: func(ctx context.Context, message string) error {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
				return &schema.LLMError{
					Module: "WebSocketExecutor",
					Method: "sendText",
					Reason: schema.NewTransportReason(fmt.Sprintf("Failed to send WebSocket message: %v", err), schema.WithURL(input.URL)),
				}
			}
			return nil
		},
		Messages: func() iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for msg := range msgCh {
					switch v := msg.(type) {
					case error:
						if !yield(nil, v) {
							return
						}
					default:
						if !yield(msg, nil) {
							return
						}
					}
				}
			}
		},
		Close: func() error {
			close(done)
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
			return err
		},
	}, nil
}

type JsonPrepared struct {
	URL     string
	Headers map[string]string
	Message string
}

type JsonInput struct {
	ToMessage     func(body any) (any, error)
	EncodeMessage func(message any) string
}

type JsonTransport struct {
	ID      string
	With    func(patch JsonInput) JsonTransport
	Prepare func(input route.TransportPrepareInput) (JsonPrepared, error)
	Frames  func(prepared JsonPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[string, error]
}

func NewJsonTransport(input JsonInput) JsonTransport {
	base := JsonTransport{
		ID: "websocket-json",
		With: func(patch JsonInput) JsonTransport {
			merged := input
			if patch.ToMessage != nil {
				merged.ToMessage = patch.ToMessage
			}
			if patch.EncodeMessage != nil {
				merged.EncodeMessage = patch.EncodeMessage
			}
			return NewJsonTransport(merged)
		},
		Prepare: func(pi route.TransportPrepareInput) (JsonPrepared, error) {
			parts, err := jsonRequestParts(pi)
			if err != nil {
				return JsonPrepared{}, err
			}
			wsURL, err := webSocketURL(parts.URL)
			if err != nil {
				return JsonPrepared{}, err
			}
			msg, err := input.ToMessage(parts.JSONBody)
			if err != nil {
				return JsonPrepared{}, err
			}
			return JsonPrepared{
				URL:     wsURL,
				Headers: parts.Headers,
				Message: input.EncodeMessage(msg),
			}, nil
		},
		Frames: func(prepared JsonPrepared, request *schema.LLMRequest, runtime route.TransportRuntime) iter.Seq2[string, error] {
			return func(yield func(string, error) bool) {
				conn, err := OpenWebSocket(context.Background(), WebSocketRequest{
					URL:     prepared.URL,
					Headers: prepared.Headers,
				})
				if err != nil {
					yield("", err)
					return
				}
				defer conn.Close()
				if err := conn.SendText(context.Background(), prepared.Message); err != nil {
					yield("", err)
					return
				}
				for msg, err := range conn.Messages() {
					if err != nil {
						if !yield("", err) {
							return
						}
						return
					}
					s := messageText(msg)
					if strings.TrimSpace(s) == "" {
						continue
					}
					if !yield(s, nil) {
						return
					}
				}
			}
		},
	}
	return base
}
