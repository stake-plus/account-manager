package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
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

	emoji := "📈"
	if changeType == "decrease" {
		emoji = "📉"
	}

	change := new(big.Int).Sub(after, before)

	msg := fmt.Sprintf("**%s Balance Change Alert**\n", emoji)
	msg += fmt.Sprintf("Account: `%s`\n", formatAddress(account))
	msg += fmt.Sprintf("Network: %s | Token: %s\n", network, token)
	msg += fmt.Sprintf("Change: %s\n", formatBalance(change, token))
	msg += fmt.Sprintf("Before: %s → After: %s",
		formatBalance(before, token), formatBalance(after, token))

	return c.sendMessage(msg, true)
}

func (c *Client) SendChildBountyAlert(account, network string, bountyID, childBountyID uint64, amount *big.Int, token string) error {
	if c == nil {
		return nil
	}

	msg := fmt.Sprintf("**🎁 Child Bounty Ready to Claim!**\n")
	msg += fmt.Sprintf("Beneficiary: `%s`\n", formatAddress(account))
	msg += fmt.Sprintf("Network: %s | Token: %s\n", network, token)
	msg += fmt.Sprintf("Parent Bounty: #%d | Child Bounty: #%d\n", bountyID, childBountyID)
	msg += fmt.Sprintf("Amount: %s\n", formatBalance(amount, token))
	msg += fmt.Sprintf("Status: ✅ Ready to claim")

	return c.sendMessage(msg, true)
}

func (c *Client) SendDailySummary(summary DailySummary) error {
	if c == nil {
		return nil
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("**📊 Daily Portfolio Summary - %s**\n", time.Now().Format("2006-01-02")))
	msg.WriteString("```\n")
	msg.WriteString(fmt.Sprintf("Active Accounts: %d | Active Networks: %d\n",
		summary.TotalAccounts, summary.ActiveNetworks))
	msg.WriteString("─────────────────────────────────────────\n")

	// Portfolio totals by token
	if len(summary.TotalsByToken) > 0 {
		msg.WriteString("PORTFOLIO TOTALS BY TOKEN\n\n")
		for symbol, tokenTotal := range summary.TotalsByToken {
			if tokenTotal.Total == nil || tokenTotal.Total.Cmp(big.NewInt(0)) == 0 {
				continue
			}
			totalStr := formatTokenAmountPrecise(tokenTotal.Total, tokenTotal.Decimals, "")
			changeStr := formatTokenAmountPrecise(tokenTotal.Change, tokenTotal.Decimals, "")
			msg.WriteString(fmt.Sprintf("%-10s  Total: %15s  Change: %15s\n",
				symbol, totalStr, changeStr))
		}
		msg.WriteString("─────────────────────────────────────────\n")
	}

	// Account details
	if len(summary.AccountSummaries) > 0 {
		msg.WriteString("ACCOUNT DETAILS\n\n")
		for _, account := range summary.AccountSummaries {
			msg.WriteString(fmt.Sprintf("%s (%s)\n", account.Name, formatAddress(account.Address)))

			// Group balances by token
			tokenGroups := make(map[string][]*TokenBalance)
			for _, tb := range account.TokenBalances {
				if tb.Balance != nil && tb.Balance.Cmp(big.NewInt(0)) > 0 {
					tokenGroups[tb.Symbol] = append(tokenGroups[tb.Symbol], tb)
				}
			}

			// Display each token with its networks
			for symbol, balances := range tokenGroups {
				total := account.TotalsByToken[symbol]
				change := account.ChangesByToken[symbol]
				decimals := summary.TokenDecimals[symbol]
				if decimals == 0 {
					decimals = 10
				}

				totalStr := formatTokenAmountPrecise(total, decimals, "")
				changeStr := formatTokenAmountPrecise(change, decimals, "")
				msg.WriteString(fmt.Sprintf("  %-8s Total: %12s  Change: %12s\n",
					symbol+":", totalStr, changeStr))

				// Show network breakdown
				for _, bal := range balances {
					balStr := formatTokenAmountPrecise(bal.Balance, bal.Decimals, "")
					msg.WriteString(fmt.Sprintf("    %-20s %12s", bal.Network+":", balStr))
					if bal.Change != nil && bal.Change.Cmp(big.NewInt(0)) != 0 {
						changeStr := formatTokenAmountPrecise(bal.Change, bal.Decimals, "")
						msg.WriteString(fmt.Sprintf(" (%s)", changeStr))
					}
					msg.WriteString("\n")
				}
			}
			msg.WriteString("\n")
		}
	}

	msg.WriteString("```")

	return c.sendMessage(msg.String(), false)
}

func (c *Client) SendValidatorAlert(address, network string, alert ValidatorAlert) error {
	if c == nil {
		return nil
	}

	icon := "⚡"
	switch alert.Type {
	case "unclaimed_rewards":
		icon = "⚠️"
	case "slash":
		icon = "🚨"
	}

	msg := fmt.Sprintf("**%s Validator Alert: %s**\n", icon, alert.Type)
	msg += fmt.Sprintf("Validator: `%s`\n", formatAddress(address))
	msg += fmt.Sprintf("Network: %s\n", network)
	msg += fmt.Sprintf("%s\n", alert.Message)

	if len(alert.UnclaimedEras) > 0 {
		msg += fmt.Sprintf("Unclaimed Eras: %v\n", alert.UnclaimedEras)
	}
	if alert.UnclaimedAmount != nil {
		msg += fmt.Sprintf("Claimable: %s\n", formatBalance(alert.UnclaimedAmount, ""))
	}
	if alert.ExpiredAmount != nil {
		msg += fmt.Sprintf("Expired: %s\n", formatBalance(alert.ExpiredAmount, ""))
	}

	return c.sendMessage(msg, true)
}

func (c *Client) sendMessage(content string, isAlert bool) error {
	if c == nil {
		return nil
	}

	if c.isBot {
		return c.sendBotMessage(content, isAlert)
	}
	return c.sendWebhookMessage(content)
}

func (c *Client) sendBotMessage(content string, isAlert bool) error {
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

	_, err := c.session.ChannelMessageSend(channelID, content)
	if err != nil {
		log.Printf("Failed to send Discord bot message: %v", err)
		return err
	}

	return nil
}

func (c *Client) sendWebhookMessage(content string) error {
	if c.webhookURL == "" {
		return nil
	}

	msg := map[string]string{
		"content": content,
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

	// Convert to float for formatting (assuming 10 decimals)
	fAmount := new(big.Float).SetInt(amount)
	divisor := new(big.Float).SetFloat64(1e10)
	result := new(big.Float).Quo(fAmount, divisor)

	// Format with sign for changes
	val, _ := result.Float64()
	formatted := fmt.Sprintf("%.4f", val)

	if val >= 0 && amount.Cmp(big.NewInt(0)) > 0 {
		formatted = "+" + formatted
	}

	if token != "" {
		formatted += " " + token
	}

	return formatted
}

func formatTokenAmount(amount *big.Int, decimals uint8, symbol string) string {
	if amount == nil {
		return "0.0000"
	}

	// Convert to float for formatting
	fAmount := new(big.Float).SetInt(amount)
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	result := new(big.Float).Quo(fAmount, divisor)

	val, _ := result.Float64()
	formatted := fmt.Sprintf("%.4f", val)

	if symbol != "" {
		formatted += " " + symbol
	}

	return formatted
}

// Use string-based arithmetic to avoid float precision issues
func formatTokenAmountPrecise(amount *big.Int, decimals uint8, symbol string) string {
	if amount == nil || amount.Cmp(big.NewInt(0)) == 0 {
		return "0.0000"
	}

	// Get divisor
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	// Get whole and fractional parts
	whole := new(big.Int).Div(amount, divisor)
	remainder := new(big.Int).Mod(amount, divisor)

	// Scale the remainder to get 4 decimal places
	// We need to multiply remainder by 10000 and divide by divisor
	scaledRemainder := new(big.Int).Mul(remainder, big.NewInt(10000))
	fracPart := new(big.Int).Div(scaledRemainder, divisor)

	// Format with exactly 4 decimal places
	formatted := fmt.Sprintf("%s.%04d", whole.String(), fracPart.Int64())

	if symbol != "" {
		formatted += " " + symbol
	}

	return formatted
}

func formatAddress(address string) string {
	if len(address) <= 16 {
		return address
	}
	return fmt.Sprintf("%s...%s", address[:6], address[len(address)-6:])
}

type TokenBalance struct {
	Network   string
	Balance   *big.Int
	Symbol    string
	Decimals  uint8
	Change    *big.Int
	TokenType string
}

type TokenTotal struct {
	Symbol   string
	Total    *big.Int
	Change   *big.Int
	Decimals uint8
}

type DailySummary struct {
	TotalAccounts      int
	ActiveNetworks     int
	TotalChanges       int
	TotalsByToken      map[string]*TokenTotal
	TokenDecimals      map[string]uint8
	ChildBountyRevenue *big.Int
	ValidatorRevenue   *big.Int
	CollatorRevenue    *big.Int
	StakingRevenue     *big.Int
	AccountSummaries   []AccountSummary
}

type AccountSummary struct {
	Name           string
	Address        string
	Summary        string
	TokenBalances  []*TokenBalance
	TotalsByToken  map[string]*big.Int
	ChangesByToken map[string]*big.Int
}

type ValidatorAlert struct {
	Type            string
	Message         string
	UnclaimedEras   []uint
	UnclaimedAmount *big.Int
	ExpiredAmount   *big.Int
}
