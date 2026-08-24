package notify

import (
	"context"
	"log"
)

type Notifier interface {
	Send(context.Context, string, string)
}

type notifier struct {
	enabled bool
	logger  *log.Logger
}

func New(enabled bool, logger *log.Logger) Notifier {
	return &notifier{enabled: enabled, logger: logger}
}

func (n *notifier) Send(ctx context.Context, title, message string) {
	if !n.enabled {
		return
	}
	if err := platformNotify(ctx, title, message); err != nil && n.logger != nil {
		n.logger.Printf("系统通知发送失败: %v", err)
	}
}
