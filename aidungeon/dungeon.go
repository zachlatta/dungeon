// AI DUNGEON API CLIENT //
package aidungeon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

type Client struct {
	Email     string
	Password  string
	AuthToken string
}

func NewClient(email, password string) (Client, error) {
	body := map[string]string{
		"email":    email,
		"password": password,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return Client{}, nil
	}

	resp, err := http.Post("https://api.aidungeon.io/users", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return Client{}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Client{}, errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type LoginResp struct {
		AccessToken string `json:"accessToken"`
	}

	var loginResp LoginResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&loginResp); err != nil {
		return Client{}, nil
	}

	return Client{
		Email:     email,
		Password:  password,
		AuthToken: loginResp.AccessToken,
	}, nil
}

func (c Client) CreateSession(prompt string) (sessionId int, output string, err error) {
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

func (c Client) Input(sessionId int, text string) (output string, err error) {
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
