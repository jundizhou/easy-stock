package execution

import (
	"os"
	"path/filepath"
	"strings"
)

// KillSwitch 是执行网关的全局急停开关。任一条件为真即视为「已拉闸」：
//
//  1. 环境变量 A_STOCK_EXEC_KILLSWITCH 取真值（1/true/on/yes/engaged）；
//  2. 落点文件（killswitch.on）存在。
//
// 文件方式便于运维在不动环境的情况下即时熔断，且比环境变量更「可见、可审计」。
// 拉闸后网关拒绝一切 live 与 sim 下单动作（sim 仅允许只读预览）。
type KillSwitch struct {
	file string
}

// NewKillSwitch 构造急停开关。file 为空时退化为仅读环境变量。
func NewKillSwitch(file string) *KillSwitch {
	return &KillSwitch{file: strings.TrimSpace(file)}
}

// Engaged 返回当前是否处于熔断状态。
func (k *KillSwitch) Engaged() bool {
	if v := strings.TrimSpace(os.Getenv("A_STOCK_EXEC_KILLSWITCH")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "on", "yes", "engaged":
			return true
		}
	}
	if k.file != "" {
		if _, err := os.Stat(k.file); err == nil {
			return true
		}
	}
	return false
}

// Engage 拉闸：写入落点文件。file 为空时仅依赖环境变量，无文件可写则安全返回。
func (k *KillSwitch) Engage() error {
	if k.file == "" {
		return nil
	}
	if dir := filepath.Dir(k.file); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(k.file, []byte("engaged\n"), 0o644)
}

// Disengage 解除拉闸：删除落点文件（不存在则幂等返回）。
func (k *KillSwitch) Disengage() error {
	if k.file == "" {
		return nil
	}
	if err := os.Remove(k.file); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
