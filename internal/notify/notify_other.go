//go:build !darwin && !linux && !windows

package notify

import (
	"context"
	"errors"
)

func platformNotify(context.Context, string, string) error {
	return errors.New("当前平台不支持系统通知")
}
