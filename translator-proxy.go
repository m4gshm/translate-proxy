package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	// ycsdk "github.com/yandex-cloud/go-sdk"
)

const (
	name = "translate-proxy"
)

var (
	oAuthToken    = flag.String("oauth-token", "", "OAuth token")
	oAuthTokenUrl = flag.String("oauth-token-url", "https://oauth.yandex.ru/authorize/?response_type=token&client_id=1a6990aa636648e9b2ef855fa7bec2fb", "OAuth token URL")
	iamTokenUrl   = flag.String("iam-token-url", "https://iam.api.cloud.yandex.net/iam/v1/tokens", "IAM token URL")
	cloudsUrl     = flag.String("clouds-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/clouds", "Yandex Clouds retrieving URL")
	foldersUrl    = flag.String("cloud-folders-url", "https://resource-manager.api.cloud.yandex.net/resource-manager/v1/folders", "Yandex Cloud folders retrieving URL")

	address  = flag.String("address", "localhost:8080", "http server address")
	apiUrl   = flag.String("url", "https://translate.api.cloud.yandex.net/translate/v2/translate", "Yandex Translate API URL")
	iamToken = flag.String("iam-token", "", "IAM token")
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
	log.SetPrefix(name + ": ")

	flag.Usage = usage
	flag.Parse()

	yandex, err := NewYandexClient(&http.Client{}, *iamTokenUrl, *cloudsUrl, *foldersUrl)
	if err != nil {
		return err
	}

	if len(*iamToken) == 0 {
		if len(*oAuthToken) == 0 {
			fmt.Println("Please go to", *oAuthTokenUrl)
			fmt.Println("in order to obtain OAuth token.")
			fmt.Print("Please enter OAuth token or stay blank if you want to enter IAM token:")
			fmt.Scanln(oAuthToken)
		}

		yandex.OAuthToken = *oAuthToken
		if len(yandex.OAuthToken) != 0 {
			if token, err := yandex.RequestIamToken(); err != nil {
				return fmt.Errorf("requestIamToken; %w", err)
			} else {
				yandex.IamToken = token.IamToken
			}
		} else {
			if _, err := fmt.Printf("Please enter IAM token:"); err != nil {
				return err
			}
			fmt.Scanln(iamToken)
		}
	} else {
		yandex.IamToken = *iamToken
	}

	if len(yandex.IamToken) == 0 {
		return fmt.Errorf("IAM token must be defined")
	}

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

	fmt.Printf("Start listening %s", *address)
	return NewServer(&http.Client{}, *address, *apiUrl, *iamToken, folderId).ListenAndServe()
}

func NewServer(httpClient *http.Client, addr, apiUrl, apiKey string, folderId string) *http.Server {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	handler := NewHandler(apiUrl, apiKey, httpClient)
	r.Route("/", func(r chi.Router) {
		r.HandleFunc("/", handler.Default)
		r.Post("/", handler.Post)

	})

	return &http.Server{Addr: addr, Handler: r}
}

func NewHandler(apiUrl, apiKey string, httpClient *http.Client) *Handler {
	return &Handler{apiUrl, apiKey, httpClient}
}

type Handler struct {
	apiUrl, apiKey string
	httpClient     *http.Client
}

func (h *Handler) Default(response http.ResponseWriter, request *http.Request) {
	cors(response)
	response.WriteHeader(http.StatusOK)
}

func (h *Handler) Post(response http.ResponseWriter, request *http.Request) {
	var postErr string
	if clientReq, err := http.NewRequest("POST", h.apiUrl, request.Body); err != nil {
		logError(err)
		postErr = "client-request: " + err.Error()
	} else {
		clientReq.Header.Set("Authorization", "Bearer "+h.apiKey)
		if clientResp, err := h.httpClient.Do(clientReq); err != nil {
			logError(err)
			postErr = "client-response: " + err.Error()
		} else if bodyPayload, err := ioutil.ReadAll(clientResp.Body); err != nil {
			logError(err)
			postErr = "client-response-read: " + err.Error()
		} else {
			statusCode := clientResp.StatusCode
			logResponse(statusCode, bodyPayload)
			cors(response)
			response.WriteHeader(statusCode)
			if _, err := response.Write(bodyPayload); err != nil {
				logError(err)
				postErr = "response: " + err.Error()
			} else {
				return
			}
		}
	}
	http.Error(response, postErr, http.StatusBadRequest)
}

func cors(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Headers", "Content-Type")
}

func logError(err error) {
	log.Printf("ERROR %s", err.Error())
}

func logResponse(statusCode int, response []byte) {
	log.Printf("%d: %s", statusCode, string(response))
}
