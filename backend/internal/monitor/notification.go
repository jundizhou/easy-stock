package monitor

// Empty choices follow all globally enabled robots. Keep ordering stable for digests.
func NotificationChannels(channels []string) []string {
	if len(channels) == 0 {
		return []string{"feishu", "dingtalk"}
	}
	result := []string{}
	for _, allowed := range []string{"feishu", "dingtalk"} {
		for _, channel := range channels {
			if channel == allowed {
				result = append(result, allowed)
				break
			}
		}
	}
	return result
}
