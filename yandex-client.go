package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"time"
)

type HTTPStatusError struct {
	Code   int
	status string
	body   string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("invalid status %d : %s, response\n%s", e.Code, e.status, e.body)
}

var _ error = (*HTTPStatusError)(nil)

func NewYandexClient(configFile string, writeableConfig bool, config *Config, client *http.Client, iamTokenURL, cloudsURL, foldersURL, translateURL string) (*YandexClient, error) {
	fURL, err := url.Parse(foldersURL)
	if err != nil {
		return nil, fmt.Errorf("invalid folders URL %s; %w", foldersURL, err)
	}
	return &YandexClient{configFile: configFile, writeableConfig: writeableConfig, Config: config, client: client, iamTokenURL: iamTokenURL, cloudsURL: cloudsURL, foldersURL: *fURL, translateURL: translateURL}, nil
}

type YandexClient struct {
	configFile      string
	writeableConfig bool
	Config          *Config
	client          *http.Client
	iamTokenURL     string
	cloudsURL       string
	foldersURL      url.URL
	translateURL    string
}

func (c *YandexClient) GetClouds() (*CloudsResponse, error) {
	respPayload := new(CloudsResponse)
	if iamToken, err := c.getStoreIamToken(); err != nil {
		return nil, err
	} else if err := doGetRequest("clouds", c.client, c.cloudsURL, iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) GetCloudFolders(cloudID string) (*FoldersResponse, error) {
	f := c.foldersURL
	q := f.Query()
	q.Set("cloudId", cloudID)
	f.RawQuery = q.Encode()
	respPayload := new(FoldersResponse)
	if iamToken, err := c.getStoreIamToken(); err != nil {
		return nil, err
	} else if err := doGetRequest("cloud folders", c.client, f.String(), iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) CreateCloudFolder(cloudID, name string) (*CreateFolderResponse, error) {
	f := c.foldersURL
	reqPayload := &CreateFolderRequest{
		CloudID: cloudID,
		Name:    name,
	}
	respPayload := new(CreateFolderResponse)
	if iamToken, err := c.getStoreIamToken(); err != nil {
		return nil, err
	} else if err := doPostRequest("create folder", c.client, f.String(), iamToken, reqPayload, respPayload, false); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) GetCloudFolder(folderID string) (*GetFolderResponse, error) {
	f := c.foldersURL
	f.Path = path.Join(f.Path, folderID)

	respPayload := new(GetFolderResponse)
	if iamToken, err := c.getStoreIamToken(); err != nil {
		return nil, err
	} else if err := doGetRequest("create folder", c.client, f.String(), iamToken, respPayload); err != nil {
		return nil, err
	} else {
		return respPayload, nil
	}
}

func (c *YandexClient) RequestIamToken() (*IamTokenResponse, error) {
	method := "requestIamToken"
	respPayload := new(IamTokenResponse)
	iamTokenRequest := IamTokenRequest{YandexPassportOauthToken: c.Config.OAuthToken}
	if reqBody, err := json.Marshal(&iamTokenRequest); err != nil {
		return nil, fmt.Errorf("%s request marshal %+v: %w", method, iamTokenRequest, err)
	} else if req, err := http.NewRequest(http.MethodPost, c.iamTokenURL, bytes.NewReader(reqBody)); err != nil {
		return nil, fmt.Errorf("%s request %w", method, err)
	} else if err := doRequest(method, c.client, req, respPayload, false); err != nil {
		return nil, err
	}
	fmt.Printf("requested Yandex API IAM token %s, expired at %s\n", respPayload.IamToken, respPayload.ExpiresAt)
	return respPayload, nil
}

func (c *YandexClient) Translate(request *TranslateRequest) (*TranslateResponse, error) {
	if len(request.FolderID) == 0 {
		request.FolderID = c.Config.FolderID
	}
	resp := new(TranslateResponse)
	if iamToken, err := c.getStoreIamToken(); err != nil {
		return nil, err
	} else if err := doPostRequest("translate", c.client, c.translateURL, iamToken, request, resp, true); err != nil {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.Code == 401 {
			logDebugf("unauthorized translate request, trying to refresh token, message: %s", statusErr.Error())
			if iamToken, err = c.refreshIamToken(c.writeableConfig); err != nil {
				return nil, err
			} else if err := doPostRequest("translate", c.client, c.translateURL, iamToken, request, resp, true); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return resp, nil
}

func (c *YandexClient) GetIamToken() (string, error) {
	return c.getIamToken(false)
}

func (c *YandexClient) getStoreIamToken() (string, error) {
	return c.getIamToken(c.writeableConfig)
}

func (c *YandexClient) getIamToken(store bool) (string, error) {
	if c.Config.IsIamTokenExpired() {
		return c.refreshIamToken(store)
	}
	return c.Config.IamToken, nil
}

func (c *YandexClient) refreshIamToken(store bool) (string, error) {
	tokenResp, err := c.RequestIamToken()
	if err != nil {
		return "", fmt.Errorf("request IAM token: %w", err)
	}
	iamToken := tokenResp.IamToken
	//todo: need lock
	c.Config.UpdateIamToken(iamToken, tokenResp.ExpiresAt)
	if store {
		c.Config.Store(c.configFile)
	}
	return iamToken, nil
}

func doGetRequest[T any](methodName string, client *http.Client, url string, iamToken string, resp *T) error {
	return doAuthRequest(methodName, client, http.MethodGet, url, iamToken, nil, resp, false)
}

func doPostRequest[Req, Resp any](methodName string, client *http.Client, url string, iamToken string, req *Req, resp *Resp, logging bool) error {
	requestBody, err := json.Marshal(req)
	if logging {
		logPayload("->", requestBody)
	}
	if err != nil {
		return fmt.Errorf("request marshal %+v: %w", req, err)
	}
	return doAuthRequest(methodName, client, http.MethodPost, url, iamToken, bytes.NewReader(requestBody), resp, logging)
}

func doAuthRequest[T any](callName string, client *http.Client, httpMethod string, url string, iamToken string, reqBody io.Reader, respReceiver *T, logging bool) error {
	req, err := http.NewRequest(httpMethod, url, reqBody)
	if err != nil {
		return fmt.Errorf("%s %s request: %w", callName, httpMethod, err)
	}
	req.Header.Set("Authorization", "Bearer "+iamToken)
	return doRequest(callName, client, req, respReceiver, logging)
}

func doRequest[T any](methodName string, client *http.Client, req *http.Request, respPayload *T, logging bool) error {
	if resp, err := client.Do(req); err != nil {
		return fmt.Errorf(methodName+" response: %w", err)
	} else if resp.StatusCode != 200 {
		payload, _ := readBody(resp)
		return &HTTPStatusError{Code: resp.StatusCode, status: resp.Status, body: string(payload)}
	} else if bodyRawPayload, err := readBody(resp); err != nil {
		return fmt.Errorf(methodName+" response payload read %s: %w", string(bodyRawPayload), err)
	} else if bodyRawPayload == nil {
		return nil
	} else if err = json.Unmarshal(bodyRawPayload, respPayload); err != nil {
		return fmt.Errorf(methodName+" response payload unmarshal %s: %w", string(bodyRawPayload), err)
	} else {
		if logging {
			logPayload("<-", bodyRawPayload)
		}
		return nil
	}
}

func readBody(resp *http.Response) ([]byte, error) {
	respBody := resp.Body
	if respBody == nil {
		return nil, nil
	}
	defer func() {
		_ = respBody.Close()
	}()
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

type CreateFolderRequest struct {
	CloudID     string `json:"cloudId,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Labels      string `json:"labels,omitempty"`
}

type CreateFolderResponse struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	CreatedBy   string `json:"createdBy"`
	ModifiedAt  string `json:"modifiedAt"`
	Done        bool   `json:"done"`
	Error       struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type GetFolderResponse struct {
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
	SourceLanguageCode string   `json:"sourceLanguageCode"`
	TargetLanguageCode string   `json:"targetLanguageCode"`
}

type TranslateResponse struct {
	Translations []Translation `json:"translations"`
}

type Translation struct {
	Text                 string `json:"text"`
	DetectedLanguageCode string `json:"detectedLanguageCode"`
}
