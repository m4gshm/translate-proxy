package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

func NewYandexClient(config *Config, client *http.Client, iamTokenUrl, cloudsUrl, foldersUrl, translateUrl string) (*YandexClient, error) {
	fUrl, err := url.Parse(foldersUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid folders URL %s; %w", foldersUrl, err)
	}
	return &YandexClient{config: config, client: client, iamTokenUrl: iamTokenUrl, cloudsUrl: cloudsUrl, foldersUrl: *fUrl, translateUrl: translateUrl}, nil
}

type YandexClient struct {
	config       *Config
	client       *http.Client
	iamTokenUrl  string
	cloudsUrl    string
	foldersUrl   url.URL
	translateUrl string
}



func (c *YandexClient) RequestClouds() (*CloudsResponse, error) {
	respPayload := new(CloudsResponse)
	if iamToken, err := c.iamToken(); err != nil {
		return nil, err
	} else if err := doGetRequest("clouds", c.client, c.cloudsUrl, iamToken, respPayload); err != nil {
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
	} else if err := doGetRequest("cloud folders", c.client, f.String(), iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) RequestIamToken(oAuthToken string) (*IamTokenResponse, error) {
	method := "requestIamToken"
	respPayload := new(IamTokenResponse)
	iamTokenRequest := IamTokenRequest{YandexPassportOauthToken: oAuthToken}
	if reqBody, err := json.Marshal(&iamTokenRequest); err != nil {
		return nil, fmt.Errorf("%s request marshal %+v: %w", method, iamTokenRequest, err)
	} else if req, err := http.NewRequest(http.MethodPost, c.iamTokenUrl, bytes.NewReader(reqBody)); err != nil {
		return nil, fmt.Errorf("%s request %w", method, err)
	} else if err := doRequest("yandexPassportOauthToken", c.client, req, respPayload); err != nil {
		return nil, err
	}
	fmt.Printf("requested Yandex API IAM token %s, expired at %s\n", respPayload.IamToken, respPayload.ExpiresAt)
	return respPayload, nil
}

func (c *YandexClient) Translate(request *TranslateRequest) (*TranslateResponse, error) {
	if len(request.FolderID) == 0 {
		request.FolderID = c.config.FolderId
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("translate request marshal error %+v: %w", request, err)
	}
	resp := new(TranslateResponse)
	if iamToken, err := c.iamToken(); err != nil {
		return nil, err
	} else if err := doAuthRequest("translate", c.client, http.MethodPost, c.translateUrl, iamToken, bytes.NewReader(requestBody), resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *YandexClient) iamToken() (string, error) {
	iamToken := c.config.IamToken
	iamTokenExpired := c.config.IamTokenExpired
	if iamTokenExpired.Before(time.Now()) {
		oAuthToken := c.config.OAuthToken
		tokenResp, err := c.RequestIamToken(oAuthToken)
		if err != nil {
			return "", err
		}
		iamToken = tokenResp.IamToken
		//todo: need lock
		c.config.IamToken = iamToken
		c.config.IamTokenExpired = tokenResp.ExpiresAt

	}
	return iamToken, nil
}

func doGetRequest[T any](methodName string, client *http.Client, url string, iamToken string, resp *T) error {
	return doAuthRequest(methodName, client, http.MethodGet, url, iamToken, nil, resp)
}

func doAuthRequest[T any](callName string, client *http.Client, httpMethod string, url string, iamToken string, reqBody io.Reader, respReceiver *T) error {
	if req, err := http.NewRequest(httpMethod, url, reqBody); err != nil {
		return fmt.Errorf("%s %s request: %w", callName, httpMethod, err)
	} else {
		req.Header.Set("Authorization", "Bearer "+iamToken)
		return doRequest(callName, client, req, respReceiver)
	}
}

func doRequest[T any](methodName string, client *http.Client, req *http.Request, respPayload *T) error {
	if resp, err := client.Do(req); err != nil {
		return fmt.Errorf(methodName+" response: %w", err)
	} else if resp.StatusCode != 200 {
		payload, _ := readBody(resp)
		return fmt.Errorf("unexpected response status %s, code %d, response\n%s", resp.Status, resp.StatusCode, payload)
	} else if bodyRawPayload, err := readBody(resp); err != nil {
		return fmt.Errorf(methodName+" response payload read %s: %w", string(bodyRawPayload), err)
	} else if bodyRawPayload == nil {
		return nil
	} else if err = json.Unmarshal(bodyRawPayload, respPayload); err != nil {
		return fmt.Errorf(methodName+" response payload unmarshal %s: %w", string(bodyRawPayload), err)
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

type TranslateRequest struct {
	FolderID           string   `json:"folderId"`
	Texts              []string `json:"texts"`
	TargetLanguageCode string   `json:"targetLanguageCode"`
}

type TranslateResponse struct {
	Translations []Translations `json:"translations"`
}

type Translations struct {
	Text                 string `json:"text"`
	DetectedLanguageCode string `json:"detectedLanguageCode"`
}
