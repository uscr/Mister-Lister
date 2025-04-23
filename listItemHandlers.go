package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	maxLineLength    = 30
	maxButtonsPerRow = 6
)

func listItemsKeyboard(ctx context.Context, b *bot.Bot, list List, userID int64) (*models.InlineKeyboardMarkup, error) {
	db, err := getDb()
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	var items []ListItem
	if err := db.WithContext(ctx).
		Where("list_id = ? AND deleted_at IS NULL", list.ID).
		Order("item_order ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch items for list %d: %w", list.ID, err)
	}

	kb := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	var currentRow []models.InlineKeyboardButton
	lineLength := 0
	buttonsInRow := 0

	for _, item := range items {
		if buttonsInRow >= maxButtonsPerRow || lineLength+len(item.Name) >= maxLineLength {
			if len(currentRow) > 0 {
				kb.InlineKeyboard = append(kb.InlineKeyboard, currentRow)
			}
			currentRow = []models.InlineKeyboardButton{}
			lineLength = 0
			buttonsInRow = 0
		}

		currentRow = append(currentRow, models.InlineKeyboardButton{
			Text:         item.Name,
			CallbackData: fmt.Sprintf("deleteListElement_%d_%d", list.ID, item.ID),
		})
		lineLength += len(item.Name)
		buttonsInRow++
	}

	if len(currentRow) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, currentRow)
	}

	// Получаем URL веб-приложения из переменной окружения
	webAppURL := os.Getenv("MISTER_LISTER_WEBAPP_URL")
	if webAppURL == "" {
		errorLog.Printf("MISTER_LISTER_WEBAPP_URL is not set for user %d", userID)
		// Если URL не настроен, добавляем только остальные кнопки
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			{Text: "F5", CallbackData: "redrawList"},
			{Text: "Alt+Tab", CallbackData: "switchList"},
			{Text: "Ctrl+Z", CallbackData: "undoDeleteListElement"},
		})
		return kb, nil
	}

	// Убедимся, что URL заканчивается на /app
	if !strings.HasSuffix(webAppURL, "/app") {
		webAppURL = strings.TrimSuffix(webAppURL, "/") + "/app"
	}

	// Добавляем кнопки, включая кнопку для открытия веб-приложения
	kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
		{Text: "F5", CallbackData: "redrawList"},
		{Text: "Alt+Tab", CallbackData: "switchList"},
		{Text: "Ctrl+Z", CallbackData: "undoDeleteListElement"},
		{
			Text: "📱 App",
			WebApp: &models.WebAppInfo{
				URL: webAppURL,
			},
		},
	})

	return kb, nil
}

func drawListItemsHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
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

	kb, err := listItemsKeyboard(ctx, b, list, userID)
	if err != nil {
		errorLog.Printf("Failed to create keyboard for list %d, user %d: %v", list.ID, userID, err)
		sendMessage(ctx, b, userID, ErrCreateMenu)
		return
	}

	sendInlineKeyboard(ctx, b, userID, fmt.Sprintf("%s:", list.Name), kb)
}

func listRedraw(ctx context.Context, b *bot.Bot, update *models.Update) {
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

	kb, err := listItemsKeyboard(ctx, b, list, userID)
	if err != nil {
		errorLog.Printf("Failed to create keyboard for list %d, user %d: %v", list.ID, userID, err)
		sendMessage(ctx, b, userID, ErrCreateMenu)
		return
	}

	sendInlineKeyboard(ctx, b, userID, fmt.Sprintf("%s:", list.Name), kb)
}

func onListUndoDelete(ctx context.Context, b *bot.Bot, update *models.Update) {
	if err := answerCallback(ctx, b, update); err != nil {
		return
	}

	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	db, err := getDb()
	if err != nil {
		errorLog.Printf("Failed to get database for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrCreateMenu)
		return
	}

	list, err := getSelectedList(ctx, userID)
	if err != nil {
		errorLog.Printf("Failed to get selected list for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, ErrNoActiveList)
		return
	}

	var lastDeleted ListItem
	if err := db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND list_id = ? AND deleted_at IS NOT NULL", userID, list.ID).
		Order("deleted_at DESC").
		First(&lastDeleted).Error; err != nil {
		errorLog.Printf("No deleted items to restore for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrNoItemsToRestore)
		return
	}

	tx := db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Check for items with conflicting item_order
	var currentItems []ListItem
	if err := db.WithContext(ctx).
		Where("user_id = ? AND list_id = ? AND deleted_at IS NULL AND item_order >= ?", userID, list.ID, lastDeleted.Item_order).
		Order("item_order ASC").
		Find(&currentItems).Error; err != nil {
		tx.Rollback()
		errorLog.Printf("Failed to fetch current items for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreItem)
		return
	}

	// Shift items with item_order >= lastDeleted.Item_order
	for _, curr := range currentItems {
		if err := tx.Model(&ListItem{}).
			Where("id = ? AND list_id = ? AND user_id = ?", curr.ID, list.ID, userID).
			Update("item_order", curr.Item_order+1).Error; err != nil {
			tx.Rollback()
			errorLog.Printf("Failed to shift item %d for user %d, list %d: %v", curr.ID, userID, list.ID, err)
			sendMessage(ctx, b, userID, ErrRestoreItem)
			return
		}
	}

	// Restore the deleted item with its original item_order
	if err := tx.Unscoped().
		Model(&lastDeleted).
		Updates(map[string]interface{}{"deleted_at": nil, "item_order": lastDeleted.Item_order}).Error; err != nil {
		tx.Rollback()
		errorLog.Printf("Failed to restore item %d for user %d, list %d: %v", lastDeleted.ID, userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreItem)
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		errorLog.Printf("Failed to commit transaction for user %d, list %d: %v", userID, list.ID, err)
		sendMessage(ctx, b, userID, ErrRestoreItem)
		return
	}

	listRedraw(ctx, b, update)
}

func onListElementClick(ctx context.Context, b *bot.Bot, update *models.Update) {
	if err := answerCallback(ctx, b, update); err != nil {
		return
	}

	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	if update.CallbackQuery == nil {
		sendMessage(ctx, b, userID, ErrInvalidCallback)
		return
	}

	parts := strings.Split(update.CallbackQuery.Data, "_")
	if len(parts) != 3 {
		sendMessage(ctx, b, userID, ErrInvalidCallback)
		return
	}

	listID, err := parseInt64(parts[1])
	if err != nil {
		sendMessage(ctx, b, userID, ErrInvalidID)
		return
	}

	elementID, err := parseInt64(parts[2])
	if err != nil {
		sendMessage(ctx, b, userID, ErrInvalidID)
		return
	}

	if err := deleteListElement(ctx, userID, listID, elementID); err != nil {
		errorLog.Printf("Failed to delete item %d from list %d for user %d: %v", elementID, listID, userID, err)
		sendMessage(ctx, b, userID, ErrDeleteItem)
		return
	}

	listRedraw(ctx, b, update)
}

func listSwitch(ctx context.Context, b *bot.Bot, update *models.Update) {
	if err := answerCallback(ctx, b, update); err != nil {
		return
	}

	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	maskedSelectListHandler(ctx, b, userID)
}
