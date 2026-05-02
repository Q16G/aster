package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
)

var ErrHumanInputCancelled = errors.New("human input cancelled by user")

type humanResponse struct {
	answer    string
	cancelled bool
}

type HumanInputBridge struct {
	program atomic.Pointer[tea.Program]
	mu      sync.Mutex
	pending map[string]chan humanResponse
}

func NewHumanInputBridge() *HumanInputBridge {
	return &HumanInputBridge{
		pending: make(map[string]chan humanResponse),
	}
}

func (b *HumanInputBridge) Bind(p *tea.Program) {
	b.program.Store(p)
}

func (b *HumanInputBridge) OnHumanInput(ctx context.Context, question string, ctxMap map[string]any) (string, error) {
	requestID, _ := ctxMap["request_id"].(string)
	if requestID == "" {
		return "", fmt.Errorf("missing request_id in context")
	}

	inputType, _ := ctxMap["input_type"].(string)
	if inputType == "" {
		inputType = "text"
	}

	var options []string
	if raw, ok := ctxMap["options"]; ok {
		if arr, ok := raw.([]string); ok {
			options = arr
		} else if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					options = append(options, s)
				}
			}
		}
	}

	answerCh := make(chan humanResponse, 1)

	b.mu.Lock()
	b.pending[requestID] = answerCh
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, requestID)
		b.mu.Unlock()
	}()

	if p := b.program.Load(); p != nil {
		p.Send(HumanRequestMsg{
			RequestID: requestID,
			Question:  question,
			InputType: inputType,
			Options:   options,
			Context:   ctxMap,
		})
	}

	select {
	case resp := <-answerCh:
		if resp.cancelled {
			return "", ErrHumanInputCancelled
		}
		return resp.answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *HumanInputBridge) Respond(requestID, answer string) {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()
	if ok {
		select {
		case ch <- humanResponse{answer: answer}:
		default:
		}
	}
}

func (b *HumanInputBridge) Cancel(requestID string) {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()
	if ok {
		select {
		case ch <- humanResponse{cancelled: true}:
		default:
		}
	}
}
