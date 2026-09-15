package inflection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ClickHouseSource reads newline-delimited JSON rows from ClickHouse HTTP.
// Keeping this adapter small lets the backtest engine remain database agnostic.
type ClickHouseSource struct {
	Endpoint, Database, User, Password string
	Client                             *http.Client
}

func (s ClickHouseSource) Query(ctx context.Context, query string, out any) error {
	if s.Endpoint == "" {
		return fmt.Errorf("clickhouse endpoint is required")
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	if s.Database != "" {
		q.Set("database", s.Database)
	}
	if s.User != "" {
		q.Set("user", s.User)
	}
	if s.Password != "" {
		q.Set("password", s.Password)
	}
	q.Set("default_format", "JSONEachRow")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	if err != nil {
		return err
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("clickhouse returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
