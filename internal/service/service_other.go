//go:build !darwin && !linux && !windows

package service

import "errors"

func Install(string, string) (string, error) {
	return "", errors.New("当前平台不支持自动安装服务")
}

func Uninstall() (string, error) {
	return "", errors.New("当前平台不支持自动卸载服务")
}
