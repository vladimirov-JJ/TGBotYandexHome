package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"golang.org/x/oauth2"
)

const (
	YandexOAuthURL   = "https://oauth.yandex.ru/authorize"
	TokenURL         = "https://oauth.yandex.ru/token"
	SmartHomeAPIURL  = "https://api.iot.yandex.net/v1.0"
	TelegramBotToken = "7774640895:AAF8JYTA9yf6oXAOc4cpFg_JGQTWSdbqSHA"
	ClientID         = "8cd08973bab94d8395faa46e34c3b3eb"
	ClientSecret     = "702a7b63f5ac4549a3d9623f6c648f0f"
)

var oauthConf = &oauth2.Config{
	ClientID:     ClientID,
	ClientSecret: ClientSecret,
	Endpoint: oauth2.Endpoint{
		AuthURL:  YandexOAuthURL,
		TokenURL: TokenURL,
	},
	RedirectURL: "http://localhost:8080/oauth_callback",
	// Scopes:      []string{"iot:view", "iot:control"}, // Для чтения + управления
	// Scopes:      []string{"iot:view"},
}

type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Online bool   `json:"online"`
}

// Добавляем хранилище токенов
type TokenStorage struct {
	mu     sync.Mutex
	tokens map[int64]*oauth2.Token
}

func NewTokenStorage() *TokenStorage {
	return &TokenStorage{
		tokens: make(map[int64]*oauth2.Token),
	}
}

func (ts *TokenStorage) Save(chatID int64, token *oauth2.Token) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tokens[chatID] = token
}

func (ts *TokenStorage) Get(chatID int64) (*oauth2.Token, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	token, exists := ts.tokens[chatID]
	return token, exists
}

var (
	storage = NewTokenStorage()
)

// Добавляем HTTP-сервер для обработки callback
func startHTTPServer() {
	http.HandleFunc("/oauth_callback", handleOAuthCallback)
	go func() {
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()
}

func main() {
	startHTTPServer()
	bot, err := tgbotapi.NewBotAPI(TelegramBotToken)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = bot.RemoveWebhook()

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, _ := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if update.Message.IsCommand() {
			switch update.Message.Command() {
			// Модифицируем команду /start
			case "start":
				authURL := oauthConf.AuthCodeURL(
					fmt.Sprint(update.Message.Chat.ID),
					oauth2.SetAuthURLParam("device_id", "your-device-id"), // Опционально
					oauth2.SetAuthURLParam("device_name", "Telegram Bot"),
				)

				msg := tgbotapi.NewMessage(
					update.Message.Chat.ID,
					"Click to authorize: "+authURL,
				)
				bot.Send(msg)

				// case "start":
				// 	authURL := oauthConf.AuthCodeURL("state", oauth2.AccessTypeOffline)
				// 	msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				// 		fmt.Sprintf("Авторизуйтесь: %s", authURL))
				// 	bot.Send(msg)
				// case "devices":
				// 	token, err := getStoredToken(update.Message.Chat.ID)
				// 	if err != nil {
				// 		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				// 			"Сначала авторизуйтесь через /start")
				// 		bot.Send(msg)
				// 		continue
				// 	}
			// Обновляем обработку команды /devices
			case "devices":
				token, exists := storage.Get(update.Message.Chat.ID)
				if !exists {
					msg := tgbotapi.NewMessage(
						update.Message.Chat.ID,
						"Please authorize first using /start",
					)
					bot.Send(msg)
					continue
				}

				devices, err := getDevices(token)
				if err != nil {
					log.Printf("Error getting devices: %v", err)
					msg := tgbotapi.NewMessage(
						update.Message.Chat.ID,
						"Error fetching devices",
					)
					bot.Send(msg)
					continue
				}

				response := "Your devices:\n"
				for _, d := range devices {
					response += fmt.Sprintf("— %s (%s)\n", d.Name, d.Type)
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
				bot.Send(msg)

				// 	devices, err := getDevices(token)
				// 	if err != nil {
				// 		msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				// 			"Ошибка получения устройств")
				// 		bot.Send(msg)
				// 		continue
				// 	}

				// 	response := "Ваши устройства:\n"
				// 	for _, d := range devices {
				// 		response += fmt.Sprintf("— %s (%s)\n", d.Name, d.Type)
				// 	}
				// 	msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
				// 	bot.Send(msg)
			}
		}
	}
}

// Модифицируем обработчик callback
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	chatID, _ := strconv.ParseInt(r.URL.Query().Get("state"), 10, 64)
	code := r.URL.Query().Get("code")

	token, err := oauthConf.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Token exchange error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	storage.Save(chatID, token)
	w.Write([]byte("Authorization successful! Return to Telegram"))
}

func getDevices(token *oauth2.Token) ([]Device, error) {
	client := oauthConf.Client(context.Background(), token)
	resp, err := client.Get(fmt.Sprintf("%s/user/devices", SmartHomeAPIURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response struct {
		Devices []Device `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Devices, nil
}

// Реализуйте эти функции для работы с хранилищем
func saveToken(chatID int64, token *oauth2.Token) {
	// Сохраните токен в базу данных
}

func getStoredToken(chatID int64) (*oauth2.Token, error) {
	// Получите токен из базы данных
	return nil, fmt.Errorf("not found")
}
