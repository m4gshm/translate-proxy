package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	name = "translate-proxy"
)

var (
	address  = flag.String("address", "localhost:8080", "http server address")
	apiUrl   = flag.String("url", "https://translate.api.cloud.yandex.net/translate/v2/translate", "Yandex Translate API URL")
	apiToken = flag.String("token", "", "IAM token; must be set")
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage of "+name+":\n")
	_, _ = fmt.Fprintf(os.Stderr, "\t"+name+" [flags]\n")
	_, _ = fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetPrefix(name + ": ")

	flag.Usage = usage
	flag.Parse()

	if len(*apiToken) == 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Enter Yandex API IAM token:")
		fmt.Scanln(apiToken)
	}

	if len(*apiToken) == 0 {
		log.Fatalf("IAM token must be defined")
	}

	if err := run(); err != nil {
		log.Fatal(err.Error())
	}
}

func run() error {
	log.Printf("Start listening %s", *address)
	return NewServer(*address, *apiUrl, *apiToken).ListenAndServe()
}

func NewServer(addr, apiUrl, apiKey string) *http.Server {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	handler := NewHandler(apiUrl, apiKey)
	r.Route("/", func(r chi.Router) {
		r.HandleFunc("/", handler.Default)
		r.Post("/", handler.Post)

	})

	return &http.Server{Addr: addr, Handler: r}
}

func NewHandler(apiUrl, apiKey string) *Handler {
	return &Handler{apiUrl, apiKey, &http.Client{Transport: &http.Transport{
		// MaxIdleConns:        10,
		// MaxIdleConnsPerHost: 100,
	}}}
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
