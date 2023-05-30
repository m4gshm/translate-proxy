package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/m4gshm/expressions/error_"
	"github.com/m4gshm/expressions/error_/catch"
	"github.com/m4gshm/expressions/error_/try"
	"github.com/m4gshm/gollections/expr/use"
	"github.com/m4gshm/gollections/predicate/eq"
	"github.com/m4gshm/gollections/predicate/match"
	"github.com/m4gshm/gollections/slice"
)

const (
	name = "translate-proxy"
)

var (
	configFile    = flag.String("config-file", "", "Configuration file")
	newFolderName = flag.String("new-folder-name", name, "New cloud folder name")
	allFolders    = flag.Bool("all-folders", false, "Don't explore only active cloud folders")
	oAuthTokenURL = flag.String("oauth-token-url", "https://oauth.yandex.ru/authorize/?response_type=token&client_id=1a6990aa636648e9b2ef855fa7bec2fb", "OAuth token URL")
	iamTokenURL   = flag.String("iam-token-url", "https://iam.api.cloud.yandex.net/iam/v1/tokens", "IAM token URL")
	cloudsURL     = flag.String("clouds-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/clouds", "Yandex Clouds URL")
	foldersURL    = flag.String("cloud-folders-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/folders", "Yandex Cloud folders URL")
	translateURL  = flag.String("translate-url", "https://translate.api.cloud.yandex.net/translate/v2/translate", "Yandex Translate API URL")
	address       = flag.String("address", "localhost:8080", "http server address")
	insecure      = flag.Bool("insecure", false, "disable server certs verifying")
	accesslog     = flag.Bool("accesslog", false, "enable access log")
	tlsCertFile   = flag.String("tls-cert-file", "", "tls cert file")
	tlsKeyFile    = flag.String("tls-key-file", "", "tls key file")
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage of "+name+":\n")
	_, _ = fmt.Fprintf(os.Stderr, "\t"+name+" [flags]\n")
	_, _ = fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err.Error())
	}
}

func run() error {
	flag.Usage = usage
	flag.Parse()

	catcher := new(error_.Catcher)

	writeableConfig := configFile == nil || len(*configFile) == 0
	if writeableConfig {
		*configFile = try.Get(catcher, func() string { return path.Join(try.Gett(catcher, os.UserHomeDir), ".config", name, "config.yaml") })
	}
	config := try.Convertt(catcher, *configFile, ReadConfig)
	if err := catcher.Err; err != nil {
		return err
	}

	loadedConfig := *config

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
		},
	}
	yandex, err := NewYandexClient(*configFile, writeableConfig, config, client, *iamTokenURL, *cloudsURL, *foldersURL, *translateURL)
	if err != nil {
		return fmt.Errorf("yandex client: %w", err)
	}

	for checkedOAuth := false; !checkedOAuth; {
		if len(config.OAuthToken) == 0 {
			fmt.Println("Please go to", *oAuthTokenURL)
			fmt.Println("in order to obtain OAuth token.")
			fmt.Print("Please enter OAuth token: ")
			if _, err := fmt.Scanln(&config.OAuthToken); err != nil {
				return err
			}
		}

		//requests iam token for oauth checking
		if _, err := yandex.GetIamToken(); err != nil {
			if error_.Check(err, match.To((*HTTPStatusError).Code, eq.To(http.StatusUnauthorized))) {
				config.OAuthToken = ""
			} else {
				return err
			}
		} else {
			checkedOAuth = true
		}
	}

	if folderID, err := selectFolder(yandex, config.FolderID); err != nil {
		return err
	} else {
		config.FolderID = folderID
	}

	if writeableConfig && loadedConfig != *config {
		storedConfig := *config
		storedConfig.Store(*configFile)
	}

	server := newServer(yandex, *address, *accesslog)
	if tlsCertFile != nil && len(*tlsCertFile) > 0 && tlsKeyFile != nil && len(*tlsKeyFile) > 0 {
		fmt.Printf("Start TLS listening %s\n", *address)
		return server.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile)
	} else {
		fmt.Printf("Start listening %s\n", *address)
		return server.ListenAndServe()
	}
}

func newServer(yandex *YandexClient, addr string, accesslog bool) *http.Server {
	r := chi.NewRouter()
	if accesslog {
		r.Use(middleware.Logger)
	}
	r.Use(middleware.Recoverer)
	handler := NewHandler(yandex)
	r.Route("/", func(r chi.Router) {
		r.HandleFunc("/", handler.Default)
		r.Post("/", handler.Post)
		//old yandex translate emulation
		r.Route("/api/v1.5/tr.json/translate", func(r chi.Router) {
			r.Options("/", handler.v1_5Options)
			r.Get("/", handler.v1_5Get)
		})
	})
	return &http.Server{Addr: addr, Handler: r}
}

func NewHandler(yandex *YandexClient) *Handler {
	return &Handler{yandex: yandex}
}

type Handler struct {
	yandex *YandexClient
}

func (h *Handler) Default(response http.ResponseWriter, request *http.Request) {
	cors(response)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok"))
}

func (h *Handler) Post(response http.ResponseWriter, request *http.Request) {
	catcher := new(error_.Catcher)
	body := try.Convertt[any](catcher, try.Convertt(catcher, try.Convertt(catcher, request, extractTranslateRequest), h.translate), json.Marshal)
	if catcher.Err == nil {
		cors(response)
		response.WriteHeader(http.StatusOK)
		_, catcher.Err = response.Write(body)
	}
	if err := catcher.Err; err != nil {
		writeError(response, err)
	}
}

func (h *Handler) v1_5Options(response http.ResponseWriter, request *http.Request) {
	response.Header().Add("Allow", "GET,OPTIONS")
	response.WriteHeader(http.StatusOK)
}

func (h *Handler) v1_5Get(response http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()

	catcher, srcLang, destLang := catch.Two(splitSrcDestLanguages(q.Get("lang")))

	body := try.Convertt[any](catcher, try.Convert(catcher, try.Convertt(catcher, &TranslateRequest{
		Texts: []string{q.Get("text")}, SourceLanguageCode: srcLang, TargetLanguageCode: destLang,
	}, h.translate), toV1_5Response), json.Marshal)

	if catcher.Err == nil {
		cors(response)
		response.WriteHeader(http.StatusOK)
		_, catcher.Err = response.Write(body)
	}
	if err := catcher.Err; err != nil {
		writeError(response, err)
	}
}

func writeError(response http.ResponseWriter, err error) {
	logError(err)
	http.Error(response, err.Error(), http.StatusBadRequest)
}

func toV1_5Response(result *TranslateResponse) *V1_5TranslateResponse {
	return &V1_5TranslateResponse{
		Text: slice.Convert(result.Translations, func(t Translation) string { return t.Text }),
	}
}

type V1_5TranslateResponse struct {
	Text []string `json:"text"`
}

func extractTranslateRequest(request *http.Request) (*TranslateRequest, error) {
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("request read: %w", err)
	}

	payload := new(TranslateRequest)
	if err = json.Unmarshal(body, payload); err != nil {
		return nil, fmt.Errorf("request unmarshal: %w", err)
	}

	payload.SourceLanguageCode = extractLanguage(payload.SourceLanguageCode)
	payload.TargetLanguageCode = extractLanguage(payload.TargetLanguageCode)
	return payload, nil
}

func (h *Handler) translate(payload *TranslateRequest) (*TranslateResponse, error) {
	return h.yandex.Translate(payload)
}

func extractLanguage(langCountry string) string {
	if strings.Contains(langCountry, "-") {
		return strings.Split(langCountry, "-")[0]
	}
	return langCountry
}

func splitSrcDestLanguages(language string) (srcLang string, destLang string, err error) {
	if len(language) == 0 {
		err = fmt.Errorf("empty source-destination languages format (expected SRC-DST)")
	} else if !strings.Contains(language, "-") {
		err = fmt.Errorf("bad source-destination languages format %s (expected SRC-DST)", language)
	} else if ls := strings.Split(language, "-"); len(ls) != 2 {
		err = fmt.Errorf("unexpected source-destination languages format %s (expected SRC-DST)", language)
	} else if srcLang, destLang = ls[0], ls[1]; len(srcLang) == 0 {
		err = fmt.Errorf("bad source language: %s", language)
	} else if len(destLang) == 0 {
		err = fmt.Errorf("bad destination language: %s", language)
	}
	return srcLang, destLang, err
}

func cors(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Headers", "Content-Type")
}

func selectFolder(yandex *YandexClient, folderID string) (string, error) {
	repeat := true
	for repeat {
		repeat = false
		if len(folderID) == 0 {
			var cloudID string
			if clouds, err := yandex.GetClouds(); err != nil {
				return "", err
			} else if clouds == nil || len(clouds.Clouds) == 0 {
				return "", errors.New("threre is no cloud for your account. Please create it")
			} else if len(clouds.Clouds) == 1 {
				cloud := clouds.Clouds[0]
				fmt.Printf("cloud %s (id = %s) automatically selected\n", cloud.Name, cloud.ID)
				cloudID = cloud.ID
			} else {
				fmt.Println("Please select cloud to use:")
				for i, cloud := range clouds.Clouds {
					n := i + 1
					fmt.Printf("[%d] cloud%d (id = %s, name = %s)\n", n, n, cloud.ID, cloud.Name)
				}
				fmt.Print("Please enter your numeric choice: ")
				var cloudNum int
				if _, err := fmt.Scanln(&cloudNum); err != nil {
					return "", err
				}
				for {
					if cloudNum > 0 && cloudNum <= len(clouds.Clouds) {
						cloud := clouds.Clouds[cloudNum-1]
						cloudID = cloud.ID
						break
					} else {
						fmt.Printf("Entered invalid cloud number, must be in the range  %d to %d\n", 1, len(clouds.Clouds))
					}
				}
			}

			if folders, err := yandex.GetCloudFolders(cloudID); err != nil {
				return "", err
			} else if folders == nil || len(folders.Folders) == 0 {
				if folderID, err = createFolder(yandex, cloudID, *newFolderName); err != nil {
					return "", err
				}
			} else {
				selectedFolders := use.If(*allFolders, folders.Folders).ElseGet(func() []Folder {
					return slice.Filter(folders.Folders, func(f Folder) bool { return f.Status == "ACTIVE" })
				})

				if len(selectedFolders) == 1 {
					folder := selectedFolders[0]
					fmt.Printf("folder %s (id = %s, status = %s) automatically selected\n", folder.Name, folder.ID, folder.Status)
					folderID = folder.ID
				} else if folderID, err = getUserSelectedFolder(selectedFolders); err != nil {
					return "", err
				}
			}
		} else if _, err := yandex.GetCloudFolder(folderID); error_.Check(err, match.To((*HTTPStatusError).Code, eq.To(http.StatusNotFound))) {
			logDebugf("configured folder %s not found", folderID)
			folderID = ""
			repeat = true
		} else if err != nil {
			return "", err
		}
	}
	return folderID, nil
}

func getUserSelectedFolder(selectedFolders []Folder) (string, error) {
	fmt.Println("Please choose a folder to use:")
	for i, folder := range selectedFolders {
		n := i + 1
		fmt.Printf("[%d] folder%d (id = %s, name = %s, status = %s)\n", n, n, folder.ID, folder.Name, folder.Status)
	}
	fmt.Println("Please enter your numeric choice: ")
	for {
		var folderNum int
		if _, err := fmt.Scanln(&folderNum); err != nil {
			return "", err
		}
		if folderNum > 0 && folderNum <= len(selectedFolders) {
			return selectedFolders[folderNum-1].ID, nil
		} else {
			fmt.Printf("Entered invalid folder number, must be in the range  %d to %d\n", 1, len(selectedFolders))
		}
	}
}

func createFolder(yandex *YandexClient, cloudID, folderName string) (string, error) {
	logDebugf("trying to create folder %s", folderName)
	// catcher, resp := catch(yandex.CreateCloudFolder(cloudID, folderName))

	resp, err := yandex.CreateCloudFolder(cloudID, folderName)
	if err != nil {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.code == http.StatusConflict {
			logDebugf("cannot create folder %s because it conflicts with some one might may be has marked as deleted", folderName)
			fmt.Print("Please enter your new folder name: ")
			if _, err := fmt.Scanln(&folderName); err != nil {
				return "", err
			} else if resp, err = yandex.CreateCloudFolder(cloudID, folderName); err != nil {
				return "", fmt.Errorf("create cloud folder %s: %w", folderName, err)
			}
		} else {
			return "", fmt.Errorf("create cloud folder %s: %w", folderName, err)
		}
	}
	if resp.Done {
		fmt.Printf("folder %s (id = %s) automatically created\n", folderName, resp.ID)
		return resp.ID, nil
	} else {
		return "", fmt.Errorf("create cloud folder %s error code %s, %s", folderName, resp.Error.Code, resp.Error.Message)
	}
}
