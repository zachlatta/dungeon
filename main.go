package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fabioberger/airtable-go"
	"github.com/joho/godotenv"
	"github.com/nlopes/slack"
)

const CostToPlay = 5 // in GP

// AIRTABLE DB //

type DB struct {
	client *airtable.Client
}

func NewDB(airtableAPIKey, airtableBaseID string) (*DB, error) {
	client, err := airtable.New(airtableAPIKey, airtableBaseID)
	if err != nil {
		return nil, err
	}

	return &DB{
		client: client,
	}, nil
}

type SlackUser struct {
	ID   string
	Name string
}

func (u SlackUser) Eq(ou SlackUser) bool {
	return u.ID == ou.ID
}

func (u SlackUser) ToString() string {
	return u.Name + " <@" + u.ID + ">"
}

// (?U) makes it non-greedy
var slackUserRegex = regexp.MustCompile("(?U)((.+) )?<@([A-Z0-9]+)>")

func SlackUserFromID(client *slack.Client, slackID string) (SlackUser, error) {
	userProfile, err := client.GetUserProfile(slackID, false)
	if err != nil {
		return SlackUser{}, err
	}

	return SlackUser{
		ID:   slackID,
		Name: userProfile.DisplayName,
	}, nil
}

func SlackUserFromString(str string) (SlackUser, error) {
	matches := slackUserRegex.FindStringSubmatch(str)
	if matches == nil {
		return SlackUser{}, errors.New("no slack user matches found")
	}

	return SlackUser{
		Name: matches[2],
		ID:   matches[3],
	}, nil
}

func SlackUsersToString(users []SlackUser) string {
	strs := make([]string, len(users))
	for i, u := range users {
		strs[i] = u.ToString()
	}

	return strings.Join(strs, ", ")
}

func SlackUsersFromIDs(client *slack.Client, ids []string) ([]SlackUser, error) {
	users := make([]SlackUser, len(ids))
	for i, id := range ids {
		var err error
		users[i], err = SlackUserFromID(client, id)
		if err != nil {
			return nil, err
		}
	}

	return users, nil
}

func SlackUsersFromString(str string) ([]SlackUser, error) {
	matches := slackUserRegex.FindAllStringSubmatch(str, -1)
	if matches == nil {
		return nil, errors.New("no slack user matches found")
	}

	fmt.Println(str)
	fmt.Println(matches)

	slackUsers := make([]SlackUser, len(matches))
	for i, match := range matches {
		slackUsers[i] = SlackUser{
			Name: match[2],
			ID:   match[3],
		}
	}

	return slackUsers, nil
}

type Session struct {
	AirtableID      string
	ThreadTimestamp string
	Creator         SlackUser
	Companions      []SlackUser
	CostGP          int
	Paid            bool
	Prompt          string
	SessionID       int
}

type airtableSession struct {
	AirtableID string `json:"id,omitempty"`
	Fields     struct {
		ThreadTimestamp string `json:"Thread Timestamp"`
		Creator         string
		Companions      string
		Cost            int  `json:"Cost (GP)"`
		Paid            bool `json:"Paid?"`
		Prompt          string
		SessionID       int `json:"Session ID,omitempty"`
	} `json:"fields"`
}

func sessionFromAirtable(as airtableSession) (Session, error) {
	creator, err := SlackUserFromString(as.Fields.Creator)
	if err != nil {
		return Session{}, err
	}

	var companions []SlackUser
	if as.Fields.Companions != "" {
		companions, err = SlackUsersFromString(as.Fields.Companions)
		if err != nil {
			return Session{}, err
		}
	}

	return Session{
		AirtableID:      as.AirtableID,
		ThreadTimestamp: as.Fields.ThreadTimestamp,
		Creator:         creator,
		Companions:      companions,
		CostGP:          as.Fields.Cost,
		Paid:            as.Fields.Paid,
		Prompt:          as.Fields.Prompt,
		SessionID:       as.Fields.SessionID,
	}, nil
}

func (db *DB) CreateSession(threadTs string, creator SlackUser, companions []SlackUser, costGP int, prompt string) (Session, error) {
	as := airtableSession{}
	as.Fields.ThreadTimestamp = threadTs
	as.Fields.Creator = creator.ToString()
	as.Fields.Companions = SlackUsersToString(companions)
	as.Fields.Cost = costGP
	as.Fields.Prompt = prompt

	if err := db.client.CreateRecord("Sessions", &as); err != nil {
		return Session{}, err
	}

	return sessionFromAirtable(as)
}

func (db *DB) GetSession(threadTs string) (Session, error) {
	listParams := airtable.ListParameters{
		// TODO Prevent string escaping problems
		FilterByFormula: `{Thread Timestamp} = "` + threadTs + `"`,
	}

	airtableSessions := []airtableSession{}
	if err := db.client.ListRecords("Sessions", &airtableSessions, listParams); err != nil {
		return Session{}, err
	}

	if len(airtableSessions) > 1 {
		return Session{}, errors.New("too many sessions, non-unique timestamps")
	} else if len(airtableSessions) == 0 {
		return Session{}, errors.New("no session found")
	}

	return sessionFromAirtable(airtableSessions[0])
}

func (db *DB) MarkSessionPaidAndStarted(session Session, sessionID int) (Session, error) {
	as := airtableSession{}

	updatedFields := map[string]interface{}{
		"Paid?":      true,
		"Session ID": sessionID,
	}

	if err := db.client.UpdateRecord("Sessions", session.AirtableID, updatedFields, &as); err != nil {
		return Session{}, err
	}

	return sessionFromAirtable(as)
}

type airtableStoryItem struct {
	AirtableID string `json:"id,omitempty"`
	Fields     struct {
		Session []string
		Type    string
		Author  string
		Value   string
	} `json:"fields"`
}

// author should be nil
func (db *DB) CreateStoryItem(session Session, itemType string, author *SlackUser, value string) error {
	si := airtableStoryItem{}
	si.Fields.Session = []string{session.AirtableID}
	si.Fields.Type = itemType

	if author != nil {
		si.Fields.Author = author.ToString()
	}

	si.Fields.Value = value

	if err := db.client.CreateRecord("Story Items", &si); err != nil {
		return err
	}

	return nil
}

// AI DUNGEON API CLIENT //

type AIDungeonClient struct {
	Email     string
	Password  string
	AuthToken string
}

func NewAIDungeonClient(email, password string) (AIDungeonClient, error) {
	body := map[string]string{
		"email":    email,
		"password": password,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return AIDungeonClient{}, nil
	}

	resp, err := http.Post("https://api.aidungeon.io/users", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return AIDungeonClient{}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return AIDungeonClient{}, errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type LoginResp struct {
		AccessToken string `json:"accessToken"`
	}

	var loginResp LoginResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&loginResp); err != nil {
		return AIDungeonClient{}, nil
	}

	return AIDungeonClient{
		Email:     email,
		Password:  password,
		AuthToken: loginResp.AccessToken,
	}, nil
}

func (c AIDungeonClient) CreateSession(prompt string) (sessionId int, output string, err error) {
	body := map[string]interface{}{
		"storyMode":     "custom",
		"characterType": nil,
		"name":          nil,
		"customPrompt":  &prompt,
		"promptId":      nil,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}

	client := &http.Client{}

	req, err := http.NewRequest("POST", "https://api.aidungeon.io/sessions", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, "", err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("x-access-token", c.AuthToken)

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, "", errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type NewSessionResp struct {
		ID    int `json:"id"`
		Story []struct {
			Value string `json:"value"`
		} `json:"story"`
	}

	var newSessionResp NewSessionResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&newSessionResp); err != nil {
		return 0, "", err
	}

	if len(newSessionResp.Story) == 0 {
		return 0, "", errors.New("story is empty for some reason")
	}

	return newSessionResp.ID, newSessionResp.Story[0].Value, nil
}

func (c AIDungeonClient) Input(sessionId int, text string) (output string, err error) {
	body := map[string]string{
		"text": text,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	client := &http.Client{}

	req, err := http.NewRequest("POST", "https://api.aidungeon.io/sessions/"+strconv.Itoa(sessionId)+"/inputs", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("x-access-token", c.AuthToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(os.Stdout, resp.Body)
		return "", errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type InputResp []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}

	var inputResp InputResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&inputResp); err != nil {
		return "", err
	}

	if len(inputResp) == 0 {
		return "", errors.New("resp length is zero for some reason...")
	}

	last := inputResp[len(inputResp)-1]
	if last.Type == "input" {
		return "", errors.New("last type is input instead of output...")
	}

	return last.Value, nil
}

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

	// check the details, make sure this transfer actually happened for the right amount
	// TODO: figure out better way to work with banker user ID TODO: dynamically get our user ID
	if m.User != "UH50T81A6" || recipientUserID != "USH186XSP" || gpAmount != CostToPlay {
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

// MAIN LOGIC //

func handleSlackError(rtm *slack.RTM, msg Msg, err error) {
	log.Println("slack api error:", err)
	rtm.SendMessage(rtm.NewOutgoingMessage(
		"Sorry, I'm having trouble connecting to Slack. Try again? (slack error)",
		msg.ChannelID(),
		slack.RTMsgOptionTS(msg.Timestamp()),
	))
}

func handleDBError(rtm *slack.RTM, msg Msg, err error) {
	log.Println("airtable api error:", err)
	rtm.SendMessage(rtm.NewOutgoingMessage(
		"Gosh, I'm having trouble with my brain right now. Sorry about that. Try again? (db error)",
		msg.ChannelID(),
		slack.RTMsgOptionTS(msg.Timestamp()),
	))
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	slackAuthToken := os.Getenv("SLACK_LEGACY_TOKEN")
	aidungeonEmail := os.Getenv("AIDUNGEON_EMAIL")
	aidungeonPassword := os.Getenv("AIDUNGEON_PASSWORD")
	airtableAPIKey := os.Getenv("AIRTABLE_API_KEY")
	airtableBaseID := os.Getenv("AIRTABLE_BASE")

	log.Println("logging into ai dungeon with email", aidungeonEmail)

	aidungeon, err := NewAIDungeonClient(aidungeonEmail, aidungeonPassword)
	if err != nil {
		log.Fatal("error connecting to aidungeon:", err)
	}

	log.Println("logged into ai dungeon")

	log.Println("authenticating with airtable")

	db, err := NewDB(airtableAPIKey, airtableBaseID)
	if err != nil {
		log.Fatal("connecting with airtable:", err)
	}

	log.Println("authenticated with airtable")

	api := slack.New(slackAuthToken)

	rtm := api.NewRTM()
	go rtm.ManageConnection()

	for rawMsg := range rtm.IncomingEvents {
		log.Println("event received:", rawMsg)

		switch ev := rawMsg.Data.(type) {
		case *slack.MessageEvent:
			// ignore system messages
			if ev.User == "USLACKBOT" || ev.User == "" {
				continue
			}

			log.Println("raw event", ev)

			parsed := parseMessage(ev)

			switch msg := parsed.(type) {
			case *StartJourneyMsg:
				log.Println("Let's start the journey!", msg)

				log.Println("Creating session in Airtable")

				creator, err := SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					handleSlackError(rtm, msg, err)
					continue
				}

				companions, err := SlackUsersFromIDs(api, msg.CompanionIDs)
				if err != nil {
					handleSlackError(rtm, msg, err)
					continue
				}

				session, err := db.CreateSession(
					msg.Timestamp(),
					creator,
					companions,
					CostToPlay,
					msg.Prompt,
				)
				if err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				log.Println("SESSION CREATED", session)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_groggily wakes up..._",
					msg.ChannelID(),
					slack.RTMsgOptionTS(msg.Timestamp()),
				))

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"Ugh... it's been a while. My bones are rough. My bones are weak. Load me up with "+strconv.Itoa(CostToPlay)+"GP and our journey together will make your week.",
					msg.ChannelID(),
					slack.RTMsgOptionTS(msg.Timestamp()),
				))
			case *ReceiveMoneyMsg:
				log.Println("Hoo hah, I got the money:", msg)

				session, err := db.GetSession(msg.ThreadTimestamp())
				if err != nil {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"Wow, I am truly flattered. Thank you!",
						msg.ChannelID(),
						slack.RTMsgOptionTS(msg.ThreadTimestamp()),
					))
					log.Println("received money, but unable to find session:", err, "-", msg)

					continue
				}

				if session.Paid {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"This journey is already paid for, but I'll still happily take your money!",
						msg.ChannelID(),
						slack.RTMsgOptionTS(session.ThreadTimestamp),
					))
					log.Println(
						"received money for already paid session:",
						session.ThreadTimestamp,
						"-",
						msg,
					)

					continue
				}

				if msg.Reason != "" {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						`"`+strings.TrimSpace(msg.Reason)+`", huh? Hope I can live up to that. Let me think on this one...`,
						msg.ChannelID(),
						slack.RTMsgOptionTS(session.ThreadTimestamp),
					))
				} else {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"Ah, now that's a bit better. Let me think on this one...",
						msg.ChannelID(),
						slack.RTMsgOptionTS(session.ThreadTimestamp),
					))
				}

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_:musical_note: elevator music :musical_note:_",
					msg.ChannelID(),
					slack.RTMsgOptionTS(session.ThreadTimestamp),
				))

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

				sessionID, output, err := aidungeon.CreateSession(session.Prompt)
				if err != nil {
					log.Fatal("ugh, failed", err)
				}

				session, err = db.MarkSessionPaidAndStarted(session, sessionID)
				if err != nil {
					log.Fatal("failed to update airtable record:", err)
				}

				if err := db.CreateStoryItem(session, "Output", nil, output); err != nil {
					log.Fatal("failed to log output:", err)
				}

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_(remember to @mention me in your replies!)_",
					msg.ChannelID(),
					slack.RTMsgOptionTS(session.ThreadTimestamp),
				))

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					output,
					msg.ChannelID(),
					slack.RTMsgOptionTS(session.ThreadTimestamp),
				))

				fmt.Println("SESSION ID:", sessionID)

			case *InputMsg:
				log.Println("HOO HAH I GOT THE INPUT:", msg)

				session, err := db.GetSession(msg.Timestamp())
				if err != nil {
					log.Println("input attemped, unable to find session:", err, "-", msg)
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"...I'm sorry. What are you talking about? We're not on a journey together right now.",
						msg.ChannelID(),
						slack.RTMsgOptionTS(msg.Timestamp()),
					))

					continue
				}

				author, err := SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					// TODO better errors
					log.Fatal("failed to get slack user info:", err)
				}

				authedInput := false
				if session.Creator.Eq(author) {
					authedInput = true
				} else {
					for _, companion := range session.Companions {
						fmt.Println(author, companion, "-", companion.Eq(author))
						if companion.Eq(author) {
							authedInput = true
						}
					}
				}

				if !authedInput {
					log.Println("input attempted from non-creator or contributor:", author.ToString(), "-", msg.Raw())
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"...sorry my friend, but this isn't your journey to embark on.",
						msg.ChannelID(),
						slack.RTMsgOptionTS(msg.Timestamp()),
					))

					continue
				}

				if err := db.CreateStoryItem(session, "Input", &author, msg.Input); err != nil {
					log.Fatal("failed to log input:", err)
				}

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

				output, err := aidungeon.Input(session.SessionID, msg.Input)
				if err != nil {
					log.Fatal("ugh, failed", err)
				}

				if err := db.CreateStoryItem(session, "Output", nil, output); err != nil {
					log.Fatal("failed to log output:", err)
				}

				rtm.SendMessage(rtm.NewOutgoingMessage(
					output,
					msg.ChannelID(),
					slack.RTMsgOptionTS(msg.Timestamp()),
				))

			case *DMMsg:
				rtm.SendMessage(rtm.NewOutgoingMessage(
					`:wave: hi there! you can only play me in public or private channels (not in DMs). just make sure you invite me (and <@`+"UH50T81A6"+`>, so you can pay me) into the channel and then give me a prompt. some of the nice folks in slack made <#`+"CSHEL6LP5"+`>, if you want to play me there.

when you give me a prompt, just make sure to @mention my name followed by the scenario you want to start with (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`). you can even leave an incomplete sentence for me and i'll finish it for you.

here are a few scenario ideas:

• You are King George VII, a noble living in the kingdom of Larion. You have a pouch of gold and a small dagger. You are awakened by one of your servants who tells you that your keep is under attack. You look out the window and see an army of orcs marching towards your capital. They are led by a large orc named

• You are Jenny, a patient living in Chicago. You have a hospital bracelet and a pack of bandages. You wake up in an old rundown hospital with no memory of how you got there. You take a look around the room and see that it is empty except for a bed and some medical equipment. The door to your right leads out into

• You are Ada Lovelace, a courier trying to survive in a post apocalyptic world by scavenging among the ruins of what is left. You have a parcel of letters and a small pistol. It's a long and dangerous road from Boston to Charleston, but you're one of the only people who knows the roads well enough to get your parcel of letters there. You set out in the morning and

• You are Michael Jackson, a pop star and soldier trying to survive in a world filled with infected zombies everywhere. You have an automatic rifle and a grenade. Your unit lost a lot of men when the infection broke, but you've managed to keep the small town you're stationed near safe for now. You look over the town and think about how things could be better, but then you remember that's what soldiers do; they make sacrifices.
`,
					msg.ChannelID(),
				))
			case *MentionMsg:
				err := api.AddReaction("wave", slack.ItemRef{
					Channel:   msg.ChannelID(),
					Timestamp: msg.Timestamp(),
				})
				if err != nil {
					log.Fatal("failed to add reaction:", err)
				}
			case *HelpMsg:
				rtm.SendMessage(rtm.NewOutgoingMessage(
					`:wave: hi there! together, we can go on _any journey you can possibly imagine_. start me with a prompt (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`) and i'll generate the rest. you can even start with an incomplete sentence and i'll finish it for you.

once we start a journey together, provide next steps and i'll generate the story (ex. `+"`@dungeon Take out the pistol you've been hiding in your back pocket`"+`). there is no limit to what we can do. your creativity is truly the limit.

here are a few scenario ideas:

• You are King George VII, a noble living in the kingdom of Larion. You have a pouch of gold and a small dagger. You are awakened by one of your servants who tells you that your keep is under attack. You look out the window and see an army of orcs marching towards your capital. They are led by a large orc named

• You are Jenny, a patient living in Chicago. You have a hospital bracelet and a pack of bandages. You wake up in an old rundown hospital with no memory of how you got there. You take a look around the room and see that it is empty except for a bed and some medical equipment. The door to your right leads out into

• You are Ada Lovelace, a courier trying to survive in a post apocalyptic world by scavenging among the ruins of what is left. You have a parcel of letters and a small pistol. It's a long and dangerous road from Boston to Charleston, but you're one of the only people who knows the roads well enough to get your parcel of letters there. You set out in the morning and

• You are Michael Jackson, a pop star and soldier trying to survive in a world filled with infected zombies everywhere. You have an automatic rifle and a grenade. Your unit lost a lot of men when the infection broke, but you've managed to keep the small town you're stationed near safe for now. You look over the town and think about how things could be better, but then you remember that's what soldiers do; they make sacrifices.`,
					msg.ChannelID(),
					slack.RTMsgOptionTS(msg.Timestamp()),
				))
			default:
				log.Println("unable to parse message event, unknown...")
			}
		}
	}
}
