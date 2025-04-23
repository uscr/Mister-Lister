package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"gorm.io/gorm"
)

func listKeyboard(ctx context.Context, b *bot.Bot, userID int64) (*models.InlineKeyboardMarkup, error) {
	db, err := getDb()
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	var owners []ListOwners
	if err := db.WithContext(ctx).Where("user_id = ?", userID).Find(&owners).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch list owners for user %d: %w", userID, err)
	}

	if len(owners) == 0 {
		return nil, fmt.Errorf("no lists available for user %d", userID)
	}

	kb := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	var row []models.InlineKeyboardButton

	for _, owner := range owners {
		var list List
		if err := db.WithContext(ctx).First(&list, "id = ?", owner.ListID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			errorLog.Printf("Failed to fetch list %d for user %d: %v", owner.ListID, userID, err)
			return nil, fmt.Errorf("failed to fetch list %d: %w", owner.ListID, err)
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         list.Name,
			CallbackData: fmt.Sprintf("selectList_%s", list.Name),
		})
	}

	if len(row) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	} else {
		return nil, fmt.Errorf("no valid lists found for user %d", userID)
	}

	return kb, nil
}

func onListSelect(ctx context.Context, b *bot.Bot, update *models.Update) {
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
	if len(parts) != 2 {
		sendMessage(ctx, b, userID, ErrInvalidCallback)
		return
	}

	listName := parts[1]
	if err := selectListByName(ctx, userID, listName); err != nil {
		errorLog.Printf("Failed to select list '%s' for user %d: %v", listName, userID, err)
		sendMessage(ctx, b, userID, ErrSelectList)
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

func selectListHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID, err := getUserID(update)
	if err != nil {
		errorLog.Printf("Failed to get user ID: %v", err)
		return
	}

	maskedSelectListHandler(ctx, b, userID)
}

func maskedSelectListHandler(ctx context.Context, b *bot.Bot, userID int64) {
	kb, err := listKeyboard(ctx, b, userID)
	if err != nil {
		errorLog.Printf("Failed to create list keyboard for user %d: %v", userID, err)
		sendMessage(ctx, b, userID, "Нет доступных списков. Создайте новый с помощью /new <название>")
		return
	}

	sendInlineKeyboard(ctx, b, userID, MsgSelectList, kb)
	sendMessage(ctx, b, userID, MsgCreateNewList)
}

func newListHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
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

	name := strings.Join(words[1:], " ")
	if err := createList(ctx, userID, name); err != nil {
		errorLog.Printf("Failed to create list '%s' for user %d: %v", name, userID, err)
		sendMessage(ctx, b, userID, ErrCreateList)
		return
	}

	sendMessage(ctx, b, userID, fmt.Sprintf(MsgListCreated, name))
}
