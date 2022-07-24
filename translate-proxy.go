package main

import (
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

	writeableConfig := false
	if configFile == nil || len(*configFile) == 0 {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		*configFile = path.Join(homeDir, ".config", name, "config.yaml")
		writeableConfig = true
	}

	config, err := ReadConfig(*configFile)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	loadedConfig := *config

	yandex, err := NewYandexClient(*configFile, writeableConfig, config, &http.Client{}, *iamTokenURL, *cloudsURL, *foldersURL, *translateURL)
	checkedOAuth := false
	for !checkedOAuth {
		if len(config.OAuthToken) == 0 {
			fmt.Println("Please go to", *oAuthTokenURL)
			fmt.Println("in order to obtain OAuth token.")
			fmt.Print("Please enter OAuth token: ")
			if _, err := fmt.Scanln(&config.OAuthToken); err != nil {
				return err
			}
		}

		if err != nil {
			return fmt.Errorf("yandex client: %w", err)
		}

		//requests iam token for oauth checking
		if _, err := yandex.GetIamToken(); err != nil {
			var statusErr *HTTPStatusError
			if errors.As(err, &statusErr) && statusErr.Code == http.StatusUnauthorized {
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

	// yandex.Config = config
	fmt.Printf("Start listening %s\n", *address)
	return newServer(yandex, *address).ListenAndServe()
}

func newServer(yandex *YandexClient, addr string) *http.Server {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	handler := NewHandler(yandex)
	r.Route("/", func(r chi.Router) {
		r.HandleFunc("/", handler.Default)
		r.Post("/", handler.Post)

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
}

func (h *Handler) Post(response http.ResponseWriter, request *http.Request) {
	if body, err := h.translate(request); err != nil {
		logError(err)
		http.Error(response, err.Error(), http.StatusBadRequest)
	} else {
		cors(response)
		response.WriteHeader(http.StatusOK)
		if _, err := response.Write(body); err != nil {
			logError(err)
			http.Error(response, err.Error(), http.StatusBadRequest)
		}
	}
}

func (h *Handler) translate(request *http.Request) ([]byte, error) {
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

	respPayload, err := h.yandex.Translate(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(respPayload)
}

func extractLanguage(langCountry string) string {
	if strings.Contains(langCountry, "-") {
		return strings.Split(langCountry, "-")[0]
	}
	return langCountry
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
				selectedFolders := folders.Folders
				onlyActiveFolders := !*allFolders
				if onlyActiveFolders {
					selectedFolders = slice.Filter(selectedFolders, func(f Folder) bool { return f.Status == "ACTIVE" })
				}
				if len(selectedFolders) == 1 {
					folder := selectedFolders[0]
					fmt.Printf("folder %s (id = %s, status = %s) automatically selected\n", folder.Name, folder.ID, folder.Status)
					folderID = folder.ID
				} else {
					fmt.Println("Please choose a folder to use:")
					for i, folder := range selectedFolders {
						n := i + 1
						fmt.Printf("[%d] folder%d (id = %s, name = %s, status = %s)\n", n, n, folder.ID, folder.Name, folder.Status)
					}
					fmt.Println("Please enter your numeric choice: ")
					var folderNum int
					if _, err := fmt.Scanln(&folderNum); err != nil {
						return "", err
					}
					for {
						if folderNum > 0 && folderNum <= len(selectedFolders) {
							folder := selectedFolders[folderNum-1]
							folderID = folder.ID
							break
						} else {
							fmt.Printf("Entered invalid folder number, must be in the range  %d to %d\n", 1, len(selectedFolders))
						}
					}
				}
			}
		} else {
			if _, err := yandex.GetCloudFolder(folderID); err != nil {
				var statusErr *HTTPStatusError
				if errors.As(err, &statusErr) && statusErr.Code == http.StatusNotFound {
					logDebug("configured folder %s not found", folderID)
					folderID = ""
					repeat = true
				} else {
					return "", err
				}
			}
		}
	}
	return folderID, nil
}

func createFolder(yandex *YandexClient, cloudID, folderName string) (string, error) {
	logDebug("trying to create folder %s", folderName)
	resp, err := yandex.CreateCloudFolder(cloudID, folderName)
	if err != nil {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.Code == http.StatusConflict {
			logDebug("cannot create folder %s because it conflicts with some one might may be has marked as deleted", folderName)
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
