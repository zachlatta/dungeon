package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/nlopes/slack"
)

// SLACK MESSAGE PARSING //

type Msg interface {
	ChannelID() string
	Timestamp() string
	ThreadTimestamp() string
	Raw() *slack.MessageEvent
}

type StartJourneyMsg struct {
	AuthorID     string
	AuthorName   string
	CompanionIDs []string
	Prompt       string
	raw          *slack.MessageEvent
}

func (m StartJourneyMsg) ChannelID() string {
	return m.raw.Channel
}

func (m StartJourneyMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m StartJourneyMsg) ThreadTimestamp() string {
	return m.raw.Timestamp
}

func (m StartJourneyMsg) Raw() *slack.MessageEvent {
	return m.raw
}

// nil if don't process, value if proceed
// Example of messages this funciton will process:
//
//   Without companions:
//
//     <@USH186XSP> You are a lone traveler searching for a wizard in the
//     middle of a gigantic forest. You’ve been searching for days in the
//     forest and are lost. After a rough night’s sleep, you wake up groggy and
//
//   With companions:
//
//     <@USH186XSP> (with <@U0C7B14Q3> and <@UDYDSUDHV>) You are a hacker in
//     the year 2999 in the future-city of Neosporia. While sitting in your
//     high-rise apartment on floor 401, you reflect on the coming
//     end-of-the-millennium and hatch a plan: you’re going to hack the moon
//     on New Year’s Eve. You call you friend named
//
func ParseStartJourneyMsg(m *slack.MessageEvent) (*StartJourneyMsg, bool) {
	// cannot be in a thread
	if m.ThreadTimestamp != "" {
		return nil, false
	}

	// cannot be in dm
	if strings.HasPrefix(m.Channel, "D") {
		return nil, false
	}

	// 3 parts of message: 1. our user id, 2. companions, 3. the prompt to
	// start journey with
	regex := regexp.MustCompile(`^<@([A-Z0-9]+)> (\(.*\) )?(.*)$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	myUserID := matches[1]
	companionText := matches[2]
	promptText := matches[3]

	// if this doesn't look like a start journey msg, skip
	// TODO: dynamically get our user ID
	if myUserID != "USH186XSP" || promptText == "" {
		return nil, false
	}

	// extract companion IDs if present
	slackCompanionIDRegex := regexp.MustCompile(`<@([A-Z0-9]+)>`)
	rawCompanionIDResults :=
		slackCompanionIDRegex.FindAllStringSubmatch(companionText, -1)

	companionIDs := make([]string, len(rawCompanionIDResults))
	for i, companionIDResult := range rawCompanionIDResults {
		companionIDs[i] = companionIDResult[1]
	}

	return &StartJourneyMsg{
		AuthorID:     m.User,
		CompanionIDs: companionIDs,
		Prompt:       promptText,
		raw:          m,
	}, true
}

type ReceiveMoneyMsg struct {
	AuthorID    string
	RecipientID string
	GP          int
	Reason      string
	raw         *slack.MessageEvent
}

func (m ReceiveMoneyMsg) ChannelID() string {
	return m.raw.Channel
}

func (m ReceiveMoneyMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m ReceiveMoneyMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m ReceiveMoneyMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseReceiveMoneyMsg(m *slack.MessageEvent) (*ReceiveMoneyMsg, bool) {
	// must be in a thread
	if m.ThreadTimestamp == "" {
		return nil, false
	}

	// 3 parts of message: 1. GP amount, 2. GP receipient ID, 3. (optional)
	// the reason the user gave to banker for the transfer
	regex := regexp.MustCompile(`^I shall transfer ([0-9,]+)gp to <@([A-Z0-9]+)> immediately( for "(.*)")?.*$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	rawGPAmount := matches[1]
	recipientUserID := matches[2]
	reasonGiven := matches[4]

	gpAmount, err := strconv.Atoi(rawGPAmount)
	if err != nil {
		return nil, false
	}

	// make sure this is an actual transfer
	// TODO: figure out better way to work with banker user ID TODO: dynamically get our user ID
	if m.User != "UH50T81A6" || recipientUserID != "USH186XSP" {
		return nil, false
	}

	return &ReceiveMoneyMsg{
		AuthorID:    m.User,
		RecipientID: recipientUserID,
		GP:          gpAmount,
		Reason:      reasonGiven,
		raw:         m,
	}, true
}

type InputMsg struct {
	AuthorID string
	Input    string
	raw      *slack.MessageEvent
}

func (m InputMsg) ChannelID() string {
	return m.raw.Channel
}

func (m InputMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m InputMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m InputMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseInputMsg(m *slack.MessageEvent) (*InputMsg, bool) {
	// must be in a thread
	if m.ThreadTimestamp == "" {
		return nil, false
	}

	// 2 parts of message: 1. @dungeon and 2. their input for the next step
	// of the session
	regex := regexp.MustCompile(`^<@([A-Z0-9]+)> (.+)$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	toUser := matches[1]
	input := matches[2]

	// TODO if m.User != creator of thread (or their buddies, if companions enabled)
	// TODO: dynamically get @dungeon id
	if toUser != "USH186XSP" {
		return nil, false
	}

	return &InputMsg{
		AuthorID: m.User,
		Input:    input,
		raw:      m,
	}, true
}

type DMMsg struct {
	AuthorID string
	Text     string
	raw      *slack.MessageEvent
}

func (m DMMsg) ChannelID() string {
	return m.raw.Channel
}

func (m DMMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m DMMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m DMMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseDMMsg(m *slack.MessageEvent) (*DMMsg, bool) {
	// DMs have channel IDs that start with D
	if strings.HasPrefix(m.Channel, "D") {
		return &DMMsg{
			AuthorID: m.User,
			Text:     m.Text,
			raw:      m,
		}, true
	}

	return nil, false
}

// when users just type @dungeon w/o anything else
type MentionMsg struct {
	Text string
	raw  *slack.MessageEvent
}

func (m MentionMsg) ChannelID() string {
	return m.raw.Channel
}

func (m MentionMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m MentionMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m MentionMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseMentionMsg(m *slack.MessageEvent) (*MentionMsg, bool) {
	if strings.TrimSpace(m.Text) == "<@"+"USH186XSP"+">" {
		return &MentionMsg{
			Text: m.Text,
			raw:  m,
		}, true
	}

	return nil, false
}

type HelpMsg struct {
	Text string
	raw  *slack.MessageEvent
}

func (m HelpMsg) ChannelID() string {
	return m.raw.Channel
}

func (m HelpMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m HelpMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m HelpMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseHelpMsg(m *slack.MessageEvent) (*HelpMsg, bool) {
	if strings.TrimSpace(m.Text) == "<@"+"USH186XSP"+">"+" help" {
		return &HelpMsg{
			Text: m.Text,
			raw:  m,
		}, true
	}

	return nil, false
}

func parseMessage(msg *slack.MessageEvent) Msg {
	var parsed Msg
	var ok bool

	// alt flows, help / mentions / dms

	parsed, ok = ParseHelpMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseMentionMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseDMMsg(msg)
	if ok {
		return parsed
	}

	// main flows, in order flow will happen

	parsed, ok = ParseStartJourneyMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseReceiveMoneyMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseInputMsg(msg)
	if ok {
		return parsed
	}

	return nil
}
