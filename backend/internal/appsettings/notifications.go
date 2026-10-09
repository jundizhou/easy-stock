package appsettings

// Notification webhooks contain credentials and stay in the private local
// settings file. HTTP views expose only their configured status.
type NotificationChannel struct {
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
	Keyword string `json:"keyword"`
}

type NotificationEvents struct {
	StockResearch       bool `json:"stock_research"`
	PortfolioInspection bool `json:"portfolio_inspection"`
	TaskFailed          bool `json:"task_failed"`
}

type Notifications struct {
	Feishu   NotificationChannel `json:"feishu"`
	Dingtalk NotificationChannel `json:"dingtalk"`
	Events   NotificationEvents  `json:"events"`
}
