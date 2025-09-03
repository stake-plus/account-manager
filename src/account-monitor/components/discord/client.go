package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"
)

type Client struct {
	webhookURL string
	channelID  string
	httpClient *http.Client
}

type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type EmbedFooter struct {
	Text string `json:"text"`
}

type WebhookMessage struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

func NewClient(webhookURL, channelID string) *Client {
	return &Client{
		webhookURL: webhookURL,
		channelID:  channelID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) SendBalanceChangeNotification(account, network, token string, before, after *big.Int, changeType string) error {
	if c.webhookURL == "" {
		return nil
	}

	color := 0x00ff00 // Green for increase
	emoji := "📈"
	if changeType == "decrease" {
		color = 0xff0000 // Red for decrease
		emoji = "📉"
	}

	change := new(big.Int).Sub(after, before)

	embed := Embed{
		Title: fmt.Sprintf("%s Balance Change Alert", emoji),
		Color: color,
		Fields: []EmbedField{
			{
				Name:   "Account",
				Value:  account,
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  network,
				Inline: true,
			},
			{
				Name:   "Token",
				Value:  token,
				Inline: true,
			},
			{
				Name:   "Change",
				Value:  formatBalance(change, token),
				Inline: true,
			},
			{
				Name:   "Before",
				Value:  formatBalance(before, token),
				Inline: true,
			},
			{
				Name:   "After",
				Value:  formatBalance(after, token),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	return c.sendEmbed(embed)
}

func (c *Client) SendChildBountyAlert(account, network string, bountyID uint64, amount *big.Int) error {
	if c.webhookURL == "" {
		return nil
	}

	embed := Embed{
		Title: "🎁 Child Bounty Ready to Claim!",
		Color: 0x00ff00,
		Fields: []EmbedField{
			{
				Name:   "Account",
				Value:  account,
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  network,
				Inline: true,
			},
			{
				Name:   "Bounty ID",
				Value:  fmt.Sprintf("%d", bountyID),
				Inline: true,
			},
			{
				Name:   "Amount",
				Value:  formatBalance(amount, ""),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	return c.sendEmbed(embed)
}

func (c *Client) SendDailySummary(summary DailySummary) error {
	if c.webhookURL == "" {
		return nil
	}

	embed := Embed{
		Title:       "📊 Daily Revenue Summary",
		Description: fmt.Sprintf("Summary for %s", time.Now().Format("2006-01-02")),
		Color:       0x0099ff,
		Fields: []EmbedField{
			{
				Name:   "Total Accounts Monitored",
				Value:  fmt.Sprintf("%d", summary.TotalAccounts),
				Inline: true,
			},
			{
				Name:   "Active Networks",
				Value:  fmt.Sprintf("%d", summary.ActiveNetworks),
				Inline: true,
			},
			{
				Name:   "Total Balance Changes",
				Value:  fmt.Sprintf("%d", summary.TotalChanges),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add revenue breakdown
	if summary.ChildBountyRevenue != nil && summary.ChildBountyRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Child Bounty Revenue",
			Value:  formatBalance(summary.ChildBountyRevenue, ""),
			Inline: false,
		})
	}

	if summary.ValidatorRevenue != nil && summary.ValidatorRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Validator Revenue",
			Value:  formatBalance(summary.ValidatorRevenue, ""),
			Inline: false,
		})
	}

	if summary.CollatorRevenue != nil && summary.CollatorRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Collator Revenue",
			Value:  formatBalance(summary.CollatorRevenue, ""),
			Inline: false,
		})
	}

	if summary.StakingRevenue != nil && summary.StakingRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Staking Revenue",
			Value:  formatBalance(summary.StakingRevenue, ""),
			Inline: false,
		})
	}

	// Add per-account summaries
	for _, account := range summary.AccountSummaries {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   fmt.Sprintf("📍 %s", account.Name),
			Value:  account.Summary,
			Inline: false,
		})
	}

	return c.sendEmbed(embed)
}

func (c *Client) sendEmbed(embed Embed) error {
	msg := WebhookMessage{
		Embeds: []Embed{embed},
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(c.webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func formatBalance(amount *big.Int, token string) string {
	if amount == nil {
		return "0"
	}

	// Simple formatting - you may want to add decimal places based on token
	formatted := amount.String()
	if token != "" {
		formatted += " " + token
	}
	return formatted
}

type DailySummary struct {
	TotalAccounts      int
	ActiveNetworks     int
	TotalChanges       int
	ChildBountyRevenue *big.Int
	ValidatorRevenue   *big.Int
	CollatorRevenue    *big.Int
	StakingRevenue     *big.Int
	AccountSummaries   []AccountSummary
}

type AccountSummary struct {
	Name    string
	Address string
	Summary string
}
