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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	name = "translate-proxy"
)

var (
	configFile    = flag.String("config-file", "", "Configuration file")
	oAuthTokenUrl = flag.String("oauth-token-url", "https://oauth.yandex.ru/authorize/?response_type=token&client_id=1a6990aa636648e9b2ef855fa7bec2fb", "OAuth token URL")
	iamTokenUrl   = flag.String("iam-token-url", "https://iam.api.cloud.yandex.net/iam/v1/tokens", "IAM token URL")
	cloudsUrl     = flag.String("clouds-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/clouds", "Yandex Clouds retrieving URL")
	foldersUrl    = flag.String("cloud-folders-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/folders", "Yandex Cloud folders retrieving URL")
	translateUrl  = flag.String("translate-url", "https://translate.api.cloud.yandex.net/translate/v2/translate", "Yandex Translate API URL")
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
		if homeDir, err := os.UserHomeDir(); err != nil {
			return fmt.Errorf("home dir: %w", err)
		} else {
			*configFile = path.Join(homeDir, ".config", name, "config.yaml")
			writeableConfig = true
		}
	}

	config, err := ReadConfig(*configFile)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	loadedConfig := *config

	yandex, err := NewYandexClient(config, &http.Client{}, *iamTokenUrl, *cloudsUrl, *foldersUrl, *translateUrl)
	checkedOAuth := false
	for !checkedOAuth {
		if len(config.OAuthToken) == 0 {
			fmt.Println("Please go to", *oAuthTokenUrl)
			fmt.Println("in order to obtain OAuth token.")
			fmt.Print("Please enter OAuth token: ")
			fmt.Scanln(&config.OAuthToken)
		}

		if err != nil {
			return fmt.Errorf("yandex client: %w", err)
		}

		//requests iam token for oauth checking
		if _, err := yandex.GetIamToken(); err != nil {
			var statusErr *HttpStatusError
			if errors.As(err, &statusErr) && statusErr.Code == 401 {
				config.OAuthToken = ""
			} else {
				return err
			}
		} else {
			checkedOAuth = true
		}
	}

	if len(config.FolderId) == 0 {
		var cloudId string
		if clouds, err := yandex.RequestClouds(); err != nil {
			return err
		} else if clouds == nil || len(clouds.Clouds) == 0 {
			return errors.New("threre is no cloud for your account. Please create it")
		} else if len(clouds.Clouds) == 1 {
			cloud := clouds.Clouds[0]
			fmt.Printf("cloud %s (id = %s) automatically selected\n", cloud.Name, cloud.ID)
			cloudId = cloud.ID
		} else {
			fmt.Println("Please select cloud to use:")
			for i, cloud := range clouds.Clouds {
				n := i + 1
				fmt.Printf("[%d] cloud%d (id = %s, name = %s)\n", n, n, cloud.ID, cloud.Name)
			}
			fmt.Print("Please enter your numeric choice: ")
			var cloudNum int
			fmt.Scanln(&cloudNum)
			for {
				if cloudNum > 0 && cloudNum <= len(clouds.Clouds) {
					cloud := clouds.Clouds[cloudNum-1]
					cloudId = cloud.ID
					break
				} else {
					fmt.Printf("Entered invalid cloud number, must be in the range  %d to %d\n", 1, len(clouds.Clouds))
				}
			}
		}

		var folderId string
		if folders, err := yandex.RequestCloudFolders(cloudId); err != nil {
			return err
		} else if folders == nil || len(folders.Folders) == 0 {
			return errors.New("there are no folders in the cloud. Please create it")
		} else if len(folders.Folders) == 1 {
			folder := folders.Folders[0]
			fmt.Printf("folder %s (id = %s, status = %s) automatically selected\n", folder.Name, folder.ID, folder.Status)
			folderId = folder.ID
		} else {
			fmt.Print("Please choose a folder to use:")
			for i, folder := range folders.Folders {
				n := i + 1
				fmt.Printf("[%d] folder%d (id = %s, name = %s, status = %s)\n", n, n, folder.ID, folder.Name, folder.Status)
			}
			// TODO:  [3] Create a new folder
			fmt.Print("Please enter your numeric choice: ")
			var folderNum int
			fmt.Scanln(&folderNum)
			for {
				if folderNum > 0 && folderNum <= len(folders.Folders) {
					folder := folders.Folders[folderNum-1]
					folderId = folder.ID
					break
				} else {
					fmt.Printf("Entered invalid folder number, must be in the range  %d to %d\n", 1, len(folders.Folders))
				}
			}

		}
		config.FolderId = folderId
	}

	if writeableConfig && loadedConfig != *config {
		if err := config.WriteConfig(*configFile); err != nil {
			logError(fmt.Errorf("wirte config file: %w", err))
		}
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
		http.Error(response, err.Error(), http.StatusBadRequest)
	} else {
		logResponse(body)
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
	respPayload, err := h.yandex.Translate(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(respPayload)
}

func cors(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Headers", "Content-Type")
}
