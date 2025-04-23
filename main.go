package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var errorLog = log.New(os.Stderr, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(defaultHandler),
		bot.WithCallbackQueryDataHandler("deleteListElement", bot.MatchTypePrefix, onListElementClick),
		bot.WithCallbackQueryDataHandler("undoDeleteListElement", bot.MatchTypePrefix, onListUndoDelete),
		bot.WithCallbackQueryDataHandler("redrawList", bot.MatchTypePrefix, listRedraw),
		bot.WithCallbackQueryDataHandler("switchList", bot.MatchTypePrefix, listSwitch),
		bot.WithCallbackQueryDataHandler("selectList", bot.MatchTypePrefix, onListSelect),
		bot.WithCallbackQueryDataHandler("undoAllConfirm", bot.MatchTypeExact, undoAllConfirmHandler),
		bot.WithCallbackQueryDataHandler("undoAllCancel", bot.MatchTypeExact, undoAllCancelHandler),
	}

	b, err := bot.New(os.Getenv("MISTER_LISTER_TOKEN"), opts...)
	if err != nil {
		log.Fatal(err)
	}

	if err := initDb(); err != nil {
		log.Fatal(err)
	}

	// Register handlers
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, helpHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/show", bot.MatchTypeExact, drawListItemsHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/share", bot.MatchTypePrefix, shareHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/list", bot.MatchTypeExact, selectListHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/new", bot.MatchTypePrefix, newListHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/me", bot.MatchTypeExact, meHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/undo", bot.MatchTypeExact, undoHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/app", bot.MatchTypeExact, appHandler)

	// Start HTTP server for Web App and API on localhost
	go func() {
		httpPort := os.Getenv("MISTER_LISTER_WEBAPP_PORT")
		if httpPort == "" {
			httpPort = "8080"
		}
		listenAddr := "127.0.0.1:" + httpPort
		mux := http.NewServeMux()
		mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "webapp/index.html")
		})
		mux.HandleFunc("/api/items", validateTelegramAuth(b, getItemsHandler))
		mux.HandleFunc("/api/delete", validateTelegramAuth(b, deleteItemHandler))
		mux.HandleFunc("/api/reorder", validateTelegramAuth(b, reorderItemsHandler))
		log.Printf("Starting HTTP server on %s", listenAddr)
		log.Fatal(http.ListenAndServe(listenAddr, mux))
	}()

	b.Start(ctx)
}

func validateTelegramAuth(b *bot.Bot, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initData := r.Header.Get("X-Telegram-Init-Data")
		if initData == "" {
			errorLog.Printf("Missing initData for request to %s", r.URL.Path)
			http.Error(w, "Missing initData", http.StatusUnauthorized)
			return
		}

		// Validate initData
		if !validateInitData(initData, os.Getenv("MISTER_LISTER_TOKEN")) {
			errorLog.Printf("Invalid initData for request to %s: %s", r.URL.Path, initData)
			http.Error(w, "Invalid initData", http.StatusUnauthorized)
			return
		}

		// Parse initData to get userID
		userID, err := parseInitDataUserID(initData)
		if err != nil {
			errorLog.Printf("Failed to parse initData for request to %s: %v", r.URL.Path, err)
			http.Error(w, "Failed to parse initData", http.StatusUnauthorized)
			return
		}

		// Add userID to request context
		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func validateInitData(initData, botToken string) bool {
	// Parse initData as URL query string
	parsed, err := url.ParseQuery(initData)
	if err != nil {
		errorLog.Printf("Failed to parse initData: %v", err)
		return false
	}

	// Extract hash
	hash := parsed.Get("hash")
	if hash == "" {
		errorLog.Printf("Hash missing in initData")
		return false
	}
	parsed.Del("hash")

	// Create data-check-string
	var checkStrings []string
	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// Use raw value without additional encoding
		checkStrings = append(checkStrings, fmt.Sprintf("%s=%s", k, parsed.Get(k)))
	}
	dataCheckString := strings.Join(checkStrings, "\n")

	// Compute HMAC-SHA256
	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	secretKey.Write([]byte(botToken))
	hmacKey := secretKey.Sum(nil)

	hmacCheck := hmac.New(sha256.New, hmacKey)
	hmacCheck.Write([]byte(dataCheckString))
	computedHash := hex.EncodeToString(hmacCheck.Sum(nil))

	return computedHash == hash
}

func parseInitDataUserID(initData string) (int64, error) {
	parsed, err := url.ParseQuery(initData)
	if err != nil {
		return 0, fmt.Errorf("failed to parse initData: %w", err)
	}
	userData := parsed.Get("user")
	if userData == "" {
		return 0, fmt.Errorf("user data not found")
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(userData), &user); err != nil {
		return 0, fmt.Errorf("failed to parse user data: %w", err)
	}
	return user.ID, nil
}

func getItemsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value("userID").(int64)
	if !ok {
		http.Error(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	list, err := getSelectedList(r.Context(), userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		http.Error(w, ErrNoActiveList, http.StatusBadRequest)
		return
	}

	items, err := getListItems(r.Context(), userID, list.ID)
	if err != nil {
		errorLog.Printf("Failed to get items for user %d, list %d: %v", userID, list.ID, err)
		http.Error(w, "Failed to fetch items", http.StatusInternalServerError)
		return
	}

	response := struct {
		ListName string     `json:"listName"`
		Items    []ListItem `json:"items"`
	}{
		ListName: list.Name,
		Items:    items,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func deleteItemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value("userID").(int64)
	if !ok {
		http.Error(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	var req struct {
		ListID int64 `json:"listId"`
		ItemID int64 `json:"itemId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := deleteListElement(r.Context(), userID, req.ListID, req.ItemID); err != nil {
		errorLog.Printf("Failed to delete item %d from list %d for user %d: %v", req.ItemID, req.ListID, userID, err)
		http.Error(w, ErrDeleteItem, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func reorderItemsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value("userID").(int64)
	if !ok {
		http.Error(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	var req struct {
		ListID  int64   `json:"listId"`
		ItemIDs []int64 `json:"itemIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := reorderListItems(r.Context(), userID, req.ListID, req.ItemIDs); err != nil {
		errorLog.Printf("Failed to reorder items for user %d, list %d: %v", userID, req.ListID, err)
		http.Error(w, "Failed to reorder items", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func helpHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}
	sendMessage(ctx, b, userID, `Доступные команды:
* /help — Показать справку
* /show — Показать активный список
* /new <название> — Создать новый список
* /list — Выбрать активный список
* /share <id> — Поделиться списком с пользователем
* /me — Показать ваш ID
* /undo — Восстановить все удалённые элементы списка
* /app — Открыть список в приложении
Связаться с автором: @uscr0`)
}

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}
	sendMessage(ctx, b, userID, `Добро пожаловать в бот списков!
Создайте первый список: /new <название>
Доступные команды:`)
	helpHandler(ctx, b, update)
}

func meHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}
	sendMessage(ctx, b, userID, fmt.Sprintf("Ваш ID: %d", userID))
}

func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	if update.Message == nil || strings.HasPrefix(update.Message.Text, "/") {
		sendMessage(ctx, b, userID, ErrUnknownCommand)
		helpHandler(ctx, b, update)
		return
	}

	if err := addItem(ctx, userID, update.Message.Text); err != nil {
		errorLog.Printf("Failed to add item for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrAddItem)
		helpHandler(ctx, b, update)
		return
	}

	sendMessage(ctx, b, userID, fmt.Sprintf(MsgItemAdded, update.Message.Text))
	drawListItemsHandler(ctx, b, update)
}

func shareHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	if update.Message == nil {
		sendMessage(ctx, b, userID, ErrInvalidCallback)
		return
	}

	words := strings.Fields(update.Message.Text)
	if len(words) < 2 {
		helpHandler(ctx, b, update)
		return
	}

	selectedList, err := getSelectedList(ctx, userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrNoActiveList)
		return
	}

	sharedWithID, err := parseInt64(words[1])
	if err != nil {
		sendMessage(ctx, b, userID, fmt.Sprintf(ErrInvalidUserID, words[1]))
		return
	}

	if sharedWithID == userID {
		sendMessage(ctx, b, userID, ErrShareWithSelf)
		return
	}

	if err := addOwner(ctx, sharedWithID, selectedList.ID); err != nil {
		errorLog.Printf("Failed to share list %d for user %d: %v", selectedList.ID, userID, err)
		sendMessage(ctx, b, userID, ErrShareList)
		return
	}

	sendMessage(ctx, b, userID, MsgListShared)
}

func appHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	list, err := getSelectedList(ctx, userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrNoActiveList)
		return
	}

	webAppURL := os.Getenv("MISTER_LISTER_WEBAPP_URL")
	if webAppURL == "" {
		errorLog.Printf("MISTER_LISTER_WEBAPP_URL is not set for user %d", userID)
		sendMessage(ctx, b, userID, "Ошибка: Web App URL не настроен")
		return
	}

	// Ensure URL ends with /app
	if !strings.HasSuffix(webAppURL, "/app") {
		webAppURL = strings.TrimSuffix(webAppURL, "/") + "/app"
	}

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text: "Открыть приложение",
					WebApp: &models.WebAppInfo{
						URL: webAppURL,
					},
				},
			},
		},
	}
	sendInlineKeyboard(ctx, b, userID, fmt.Sprintf("Список '%s' в приложении:", list.Name), kb)
}

func undoHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	list, err := getSelectedList(ctx, userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrNoActiveList)
		return
	}

	db, err := getDb()
	if err != nil {
		errorLog.Printf("Failed to get database for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrCreateMenu)
		return
	}

	var deletedItems []ListItem
	if err := db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND list_id = ? AND deleted_at IS NOT NULL", userID, list.ID).
		Find(&deletedItems).Error; err != nil || len(deletedItems) == 0 {
		errorLog.Printf("No deleted items to restore for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrNoItemsToRestore)
		return
	}

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Подтвердить", CallbackData: "undoAllConfirm"},
				{Text: "Отменить", CallbackData: "undoAllCancel"},
			},
		},
	}
	sendInlineKeyboard(ctx, b, userID, fmt.Sprintf(MsgUndoConfirmFormat, len(deletedItems)), kb)
}

func undoAllConfirmHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if err := answerCallback(ctx, b, update); err != nil {
		return
	}

	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	list, err := getSelectedList(ctx, userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrNoActiveList)
		return
	}

	db, err := getDb()
	if err != nil {
		errorLog.Printf("Failed to get database for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrCreateMenu)
		return
	}

	var deletedItems []ListItem
	if err := db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND list_id = ? AND deleted_at IS NOT NULL", userID, list.ID).
		Order("item_order ASC").
		Find(&deletedItems).Error; err != nil {
		errorLog.Printf("Failed to fetch deleted items for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreAllItems)
		return
	}

	tx := db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get current items to check for order conflicts
	var currentItems []ListItem
	if err := db.WithContext(ctx).
		Where("list_id = ? AND deleted_at IS NULL", list.ID).
		Order("item_order ASC").
		Find(&currentItems).Error; err != nil {
		tx.Rollback()
		errorLog.Printf("Failed to fetch current items for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreAllItems)
		return
	}

	// Restore items with their original item_order, shifting conflicts
	for _, item := range deletedItems {
		// Check if item_order is occupied
		for _, curr := range currentItems {
			if curr.Item_order >= item.Item_order {
				// Shift current item's order
				if err := tx.Model(&ListItem{}).
					Where("id = ? AND list_id = ?", curr.ID, list.ID).
					Update("item_order", curr.Item_order+1).Error; err != nil {
					tx.Rollback()
					errorLog.Printf("Failed to shift item %d for user %d, list %d: %v", curr.ID, userID, list.ID, err)
					sendMessage(ctx, b, userID, ErrRestoreAllItems)
					return
				}
			}
		}

		// Restore the deleted item
		if err := tx.Unscoped().
			Model(&ListItem{}).
			Where("id = ? AND list_id = ? AND user_id = ?", item.ID, list.ID, userID).
			Updates(map[string]interface{}{"deleted_at": nil, "item_order": item.Item_order}).Error; err != nil {
			tx.Rollback()
			errorLog.Printf("Failed to restore item %d for user %d, list %d: %v", item.ID, userID, list.ID, err)
			sendMessage(ctx, b, userID, ErrRestoreAllItems)
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		errorLog.Printf("Failed to commit transaction for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreAllItems)
		return
	}

	log.Printf("Restored %d deleted items for user %d, list %d", len(deletedItems), userID, list.ID)
	sendMessage(ctx, b, userID, MsgUndoAllSuccess)
	drawListItemsHandler(ctx, b, update)
}

func undoAllCancelHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if err := answerCallback(ctx, b, update); err != nil {
		return
	}

	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	sendMessage(ctx, b, userID, MsgUndoCancelled)
}