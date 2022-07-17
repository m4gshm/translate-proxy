package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

func NewYandexClient(client *http.Client, iamTokenUrl string, cloudsUrl string, foldersUrl string) (*YandexClient, error) {
	fUrl, err := url.Parse(foldersUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid folders URL %s; %w", foldersUrl, err)
	}
	return &YandexClient{client: client, iamTokenUrl: iamTokenUrl, cloudsUrl: cloudsUrl, foldersUrl: *fUrl}, nil

}

type YandexClient struct {
	client      *http.Client
	iamTokenUrl string
	cloudsUrl   string
	foldersUrl  url.URL

	OAuthToken string
	IamToken   string
}

func (c *YandexClient) RequestClouds() (*CloudsResponse, error) {
	respPayload := new(CloudsResponse)
	if iamToken, err := c.iamToken(); err != nil {
		return nil, err
	} else if err := getRequest("clouds", c.client, c.cloudsUrl, iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) RequestCloudFolders(cloudId string) (*FoldersResponse, error) {
	f := c.foldersUrl
	q := f.Query()
	q.Set("cloudId", cloudId)
	f.RawQuery = q.Encode()
	respPayload := new(FoldersResponse)
	if iamToken, err := c.iamToken(); err != nil {
		return nil, err
	} else if err := getRequest("cloud folders", c.client, f.String(), iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) RequestIamToken() (*IamTokenResponse, error) {
	respPayload := new(IamTokenResponse)
	iamTokenRequestPayload := IamTokenRequest{YandexPassportOauthToken: c.OAuthToken}
	if iamTokenRequestBody, err := json.Marshal(&iamTokenRequestPayload); err != nil {
		return nil, fmt.Errorf("yandexPassportOauthToken request marshal error %v; %w", iamTokenRequestPayload, err)
	} else if req, err := http.NewRequest(http.MethodPost, c.iamTokenUrl, bytes.NewReader(iamTokenRequestBody)); err != nil {
		return nil, fmt.Errorf("yandexPassportOauthToken request error %w", err)
	} else if err := doRequest("yandexPassportOauthToken", c.client, req, respPayload); err != nil {
		return nil, err
	} else {
		fmt.Printf("requested Yandex API IAM token %s, expired at %s\n", respPayload.IamToken, respPayload.ExpiresAt)
		c.IamToken = respPayload.IamToken
		return respPayload, nil
	}
}

func (c *YandexClient) iamToken() (string, error) {
	return c.IamToken, nil
}

func getRequest[T any](methodName string, client *http.Client, url string, iamToken string, respPayload *T) error {
	if req, err := http.NewRequest(http.MethodGet, url, nil); err != nil {
		return fmt.Errorf(methodName+" request error: %w", err)
	} else {
		req.Header.Set("Authorization", "Bearer "+iamToken)
		return doRequest(methodName, client, req, respPayload)
	}
}

func doRequest[T any](methodName string, client *http.Client, req *http.Request, respPayload *T) error {
	if resp, err := client.Do(req); err != nil {
		return fmt.Errorf(methodName+" response error: %w", err)
	} else if resp.StatusCode != 200 {
		payload, _ := readBody(resp)
		return fmt.Errorf("unexpected response status %s, code %d, response\n%s", resp.Status, resp.StatusCode, payload)
	} else if bodyRawPayload, err := readBody(resp); err != nil {
		return fmt.Errorf(methodName+" response payload read error %s; %w", string(bodyRawPayload), err)
	} else if bodyRawPayload == nil {
		return nil
	} else if err = json.Unmarshal(bodyRawPayload, respPayload); err != nil {
		return fmt.Errorf(methodName+" response payload unmarshal error %s; %w", string(bodyRawPayload), err)
	} else {
		return nil
	}
}

func readBody(resp *http.Response) ([]byte, error) {
	respBody := resp.Body
	if respBody == nil {
		return nil, nil
	}
	defer respBody.Close()
	payload, err := ioutil.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

type IamTokenRequest struct {
	YandexPassportOauthToken string `json:"yandexPassportOauthToken"`
}

type IamTokenResponse struct {
	IamToken  string    `json:"iamToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type FoldersResponse struct {
	Folders       []Folder `json:"folders"`
	NextPageToken string   `json:"nextPageToken"`
}

type Folder struct {
	ID          string `json:"id"`
	CloudID     string `json:"cloudId"`
	CreatedAt   string `json:"createdAt"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Labels      string `json:"labels"`
	Status      string `json:"status"`
}

type CloudsResponse struct {
	Clouds        []Cloud `json:"clouds"`
	NextPageToken string  `json:"nextPageToken"`
}

type Cloud struct {
	ID             string `json:"id"`
	CreatedAt      string `json:"createdAt"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	OrganizationID string `json:"organizationId"`
	Labels         string `json:"labels"`
}
