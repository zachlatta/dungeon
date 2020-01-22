// Airtable database interaction
package db

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/fabioberger/airtable-go"
	"github.com/nlopes/slack"
)

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
		// I can't find a great way to escape values here. The best
		// thing I could find online suggested wrapping values in both
		// single and double quotes, but that breaks the search query.
		//
		// I'm just leaving as this for now and I'm not too concerned
		// with security, as the only values being pushed through this
		// should be thread tiemstamps Slack is giving directly to us.
		//
		// Source: https://community.airtable.com/t/using-api-to-filter-records-fails-when-the-value-contains-a-comma/20035/4
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
