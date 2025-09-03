package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Client struct {
	webhookURL string
	channelID  string
	httpClient *http.Client
	session    *discordgo.Session
	alertsID   string
	summaryID  string
	isBot      bool
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

func NewWebhookClient(webhookURL, channelID string) *Client {
	return &Client{
		webhookURL: webhookURL,
		channelID:  channelID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		isBot: false,
	}
}

func NewBotClient(token, alertsChannelID, summaryChannelID string) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

	if err := session.Open(); err != nil {
		return nil, fmt.Errorf("failed to open Discord connection: %w", err)
	}

	return &Client{
		session:   session,
		alertsID:  alertsChannelID,
		summaryID: summaryChannelID,
		isBot:     true,
	}, nil
}

func (c *Client) SendBalanceChangeNotification(account, network, token string, before, after *big.Int, changeType string) error {
	if c == nil {
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
				Value:  formatAddress(account),
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
		Footer: &EmbedFooter{
			Text: "Account Monitor",
		},
	}

	return c.sendEmbed(embed, true)
}

func (c *Client) SendChildBountyAlert(account, network string, bountyID, childBountyID uint64, amount *big.Int, token string) error {
	if c == nil {
		return nil
	}

	embed := Embed{
		Title: "🎁 Child Bounty Ready to Claim!",
		Color: 0x00ff00,
		Fields: []EmbedField{
			{
				Name:   "Beneficiary",
				Value:  formatAddress(account),
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  network,
				Inline: true,
			},
			{
				Name:   "Parent Bounty",
				Value:  fmt.Sprintf("#%d", bountyID),
				Inline: true,
			},
			{
				Name:   "Child Bounty",
				Value:  fmt.Sprintf("#%d", childBountyID),
				Inline: true,
			},
			{
				Name:   "Amount",
				Value:  formatBalance(amount, token),
				Inline: true,
			},
			{
				Name:   "Status",
				Value:  "✅ Ready to claim",
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Footer: &EmbedFooter{
			Text: "Account Monitor - Child Bounty Alert",
		},
	}

	return c.sendEmbed(embed, true)
}

func (c *Client) SendDailySummary(summary DailySummary) error {
	if c == nil {
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
		Footer: &EmbedFooter{
			Text: "Account Monitor - Daily Summary",
		},
	}

	// Add revenue breakdown
	if summary.ChildBountyRevenue != nil && summary.ChildBountyRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "💰 Child Bounty Revenue",
			Value:  formatBalance(summary.ChildBountyRevenue, ""),
			Inline: false,
		})
	}

	if summary.ValidatorRevenue != nil && summary.ValidatorRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "⚡ Validator Revenue",
			Value:  formatBalance(summary.ValidatorRevenue, ""),
			Inline: false,
		})
	}

	if summary.CollatorRevenue != nil && summary.CollatorRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "🔗 Collator Revenue",
			Value:  formatBalance(summary.CollatorRevenue, ""),
			Inline: false,
		})
	}

	if summary.StakingRevenue != nil && summary.StakingRevenue.Cmp(big.NewInt(0)) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "📈 Staking Revenue",
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

	// Calculate total revenue
	totalRevenue := big.NewInt(0)
	if summary.ChildBountyRevenue != nil {
		totalRevenue.Add(totalRevenue, summary.ChildBountyRevenue)
	}
	if summary.ValidatorRevenue != nil {
		totalRevenue.Add(totalRevenue, summary.ValidatorRevenue)
	}
	if summary.CollatorRevenue != nil {
		totalRevenue.Add(totalRevenue, summary.CollatorRevenue)
	}
	if summary.StakingRevenue != nil {
		totalRevenue.Add(totalRevenue, summary.StakingRevenue)
	}

	embed.Fields = append(embed.Fields, EmbedField{
		Name:   "🎯 Total Daily Revenue",
		Value:  formatBalance(totalRevenue, ""),
		Inline: false,
	})

	return c.sendEmbed(embed, false)
}

func (c *Client) SendValidatorAlert(address, network string, alert ValidatorAlert) error {
	if c == nil {
		return nil
	}

	color := 0x0099ff // Blue for info
	if alert.Type == "unclaimed_rewards" {
		color = 0xffaa00 // Orange for warning
	} else if alert.Type == "slash" {
		color = 0xff0000 // Red for slash
	}

	embed := Embed{
		Title:       fmt.Sprintf("⚡ Validator Alert: %s", alert.Type),
		Description: alert.Message,
		Color:       color,
		Fields: []EmbedField{
			{
				Name:   "Validator",
				Value:  formatAddress(address),
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  network,
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Footer: &EmbedFooter{
			Text: "Account Monitor - Validator Alert",
		},
	}

	// Add details based on alert type
	if alert.UnclaimedEras != nil && len(alert.UnclaimedEras) > 0 {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Unclaimed Eras",
			Value:  fmt.Sprintf("%v", alert.UnclaimedEras),
			Inline: false,
		})
	}

	if alert.UnclaimedAmount != nil {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Claimable Amount",
			Value:  formatBalance(alert.UnclaimedAmount, ""),
			Inline: true,
		})
	}

	if alert.ExpiredAmount != nil {
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   "Expired Amount",
			Value:  formatBalance(alert.ExpiredAmount, ""),
			Inline: true,
		})
	}

	return c.sendEmbed(embed, true)
}

func (c *Client) sendEmbed(embed Embed, isAlert bool) error {
	if c == nil {
		return nil
	}

	if c.isBot {
		return c.sendBotMessage(embed, isAlert)
	}
	return c.sendWebhookMessage(embed)
}

func (c *Client) sendBotMessage(embed Embed, isAlert bool) error {
	if c.session == nil {
		return fmt.Errorf("bot session not initialized")
	}

	channelID := c.summaryID
	if isAlert && c.alertsID != "" {
		channelID = c.alertsID
	}

	if channelID == "" {
		return fmt.Errorf("no channel ID configured")
	}

	discordEmbed := &discordgo.MessageEmbed{
		Title:       embed.Title,
		Description: embed.Description,
		Color:       embed.Color,
		Timestamp:   embed.Timestamp,
	}

	if embed.Footer != nil {
		discordEmbed.Footer = &discordgo.MessageEmbedFooter{
			Text: embed.Footer.Text,
		}
	}

	for _, field := range embed.Fields {
		discordEmbed.Fields = append(discordEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   field.Name,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}

	_, err := c.session.ChannelMessageSendEmbed(channelID, discordEmbed)
	if err != nil {
		log.Printf("Failed to send Discord bot message: %v", err)
		return err
	}

	return nil
}

func (c *Client) sendWebhookMessage(embed Embed) error {
	if c.webhookURL == "" {
		return nil
	}

	msg := WebhookMessage{
		Embeds: []Embed{embed},
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	resp, err := c.httpClient.Post(c.webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to send Discord webhook: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) Close() error {
	if c != nil && c.isBot && c.session != nil {
		return c.session.Close()
	}
	return nil
}

func formatBalance(amount *big.Int, token string) string {
	if amount == nil {
		return "0"
	}

	// Convert to float for formatting
	fAmount := new(big.Float).SetInt(amount)
	divisor := new(big.Float).SetFloat64(1e10) // Assuming 10 decimals
	result := new(big.Float).Quo(fAmount, divisor)

	formatted := fmt.Sprintf("%.4f", result)
	if token != "" {
		formatted += " " + token
	}
	return formatted
}

func formatAddress(address string) string {
	if len(address) <= 16 {
		return address
	}
	return fmt.Sprintf("%s...%s", address[:6], address[len(address)-6:])
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

type ValidatorAlert struct {
	Type            string
	Message         string
	UnclaimedEras   []uint
	UnclaimedAmount *big.Int
	ExpiredAmount   *big.Int
}
