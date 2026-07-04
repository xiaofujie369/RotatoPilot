package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Telegram struct {
	Enabled       bool
	Token, ChatID string
}

func (t Telegram) Send(message string) {
	if !t.Enabled || t.Token == "" || t.ChatID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		form := url.Values{"chat_id": {t.ChatID}, "text": {message}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.Token+"/sendMessage", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			var sink any
			_ = json.NewDecoder(resp.Body).Decode(&sink)
		}
	}()
}
