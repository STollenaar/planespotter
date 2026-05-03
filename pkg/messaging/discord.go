package messaging

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	adsbdb "github.com/nint8835/go-adsbdb"
	webhooks "github.com/typical-developers/discord-webhooks-go/v2"

	"github.com/nint8835/planespotter/pkg/tar1090"
)

// AircraftMessage contains the aircraft data used to build a Discord message.
type AircraftMessage struct {
	Aircraft tar1090.Aircraft
	Details  *adsbdb.Aircraft
	Route    *adsbdb.FlightRoute
}

// NoopSender discards aircraft messages.
type NoopSender struct{}

// SendAircraft implements an aircraft sender that does nothing.
func (NoopSender) SendAircraft(context.Context, AircraftMessage) error {
	return nil
}

// DiscordSender sends aircraft messages to a Discord webhook.
type DiscordSender struct {
	client   *webhooks.WebhookClient
	threadID string
}

// NewDiscordSender creates a sender for the provided Discord webhook URL.
func NewDiscordSender(webhookURL string, threadID string) (*DiscordSender, error) {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return nil, errors.New("discord webhook URL is required")
	}
	if _, err := url.ParseRequestURI(webhookURL); err != nil {
		return nil, fmt.Errorf("parse discord webhook URL: %w", err)
	}

	return &DiscordSender{
		client:   webhooks.NewWebhookClientFromURL(webhookURL),
		threadID: strings.TrimSpace(threadID),
	}, nil
}

// SendAircraft posts an aircraft notification to Discord.
func (s *DiscordSender) SendAircraft(ctx context.Context, message AircraftMessage) error {
	params := url.Values{}
	if s.threadID != "" {
		params.Set("thread_id", s.threadID)
	}

	_, res, err := s.client.Execute(ctx, buildPayload(message), &params)
	if res != nil && res.Body != nil {
		defer func() {
			_ = res.Body.Close()
		}()
	}
	if err != nil {
		return fmt.Errorf("execute discord webhook: %w", err)
	}
	if res != nil && (res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices) {
		return fmt.Errorf("execute discord webhook: unexpected status %s", res.Status)
	}

	return nil
}

func buildPayload(message AircraftMessage) webhooks.MessagePayload {
	aircraft := message.Aircraft
	title := firstNonEmpty(
		strings.TrimSpace(aircraft.Flight),
		strings.TrimSpace(aircraft.Registration),
		strings.ToUpper(aircraft.Hex),
	)

	embed := webhooks.Embed{
		Author:    &webhooks.EmbedAuthor{Name: "New aircraft spotted"},
		Title:     title,
		Color:     0x2f80ed,
		Fields:    fields(aircraft, message.Details, message.Route),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Footer:    footer(aircraft),
	}
	if message.Details != nil && message.Details.URLPhoto != nil {
		embed.Image = &webhooks.EmbedImage{URL: *message.Details.URLPhoto}
	}

	return webhooks.MessagePayload{
		Embeds: []webhooks.Embed{embed},
		AllowedMentions: &webhooks.AllowedMentions{
			Parse: []webhooks.AllowedMentionsParse{},
		},
	}
}

func fields(
	aircraft tar1090.Aircraft,
	details *adsbdb.Aircraft,
	route *adsbdb.FlightRoute,
) []webhooks.EmbedField {
	var fields []webhooks.EmbedField
	addField := func(name string, value string) {
		if value = strings.TrimSpace(value); value != "" {
			fields = append(fields, webhooks.EmbedField{Name: name, Value: value})
		}
	}

	addField("Aircraft", identityLine(aircraft, details))
	addField("Operator", firstNonEmpty(airline(route), detailOwner(details)))
	addField("Route", routeDescription(route))
	if len(fields) == 0 {
		addField("Aircraft", "A previously unseen aircraft was picked up by tar1090.")
	}

	return fields
}

func footer(aircraft tar1090.Aircraft) *webhooks.EmbedFooter {
	hex := strings.ToUpper(strings.TrimSpace(aircraft.Hex))
	if hex == "" {
		return nil
	}
	return &webhooks.EmbedFooter{Text: "Mode S " + hex}
}
