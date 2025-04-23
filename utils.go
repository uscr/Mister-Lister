package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Error messages
const (
	ErrInvalidCallback  = "Неверные данные команды"
	ErrNoActiveList     = "Не удалось получить активный список"
	ErrCreateMenu       = "Не удалось создать меню"
	ErrInvalidID        = "Неверный ID"
	ErrDeleteItem       = "Не удалось удалить элемент"
	ErrRestoreItem      = "Не удалось восстановить элемент"
	ErrNoItemsToRestore = "Нет удалённых элементов для восстановления"
	ErrSelectList       = "Не удалось выбрать список"
	ErrCreateList       = "Не удалось создать список"
	ErrShareList        = "Не удалось поделиться списком"
	ErrInvalidUserID    = "Укажите действительный ID пользователя"
	ErrShareWithSelf    = "Нельзя поделиться списком с самим собой"
	ErrUnknownCommand   = "Неизвестная команда"
	ErrAddItem          = "Не удалось добавить элемент"
	ErrRestoreAllItems  = "Не удалось восстановить все элементы"
)

// Messages
const (
	MsgListShared        = "Поделились списком"
	MsgItemAdded         = "Добавлено: %s"
	MsgListCreated       = "Создали список '%s' и сделали его активным"
	MsgSelectList        = "Выберите список:"
	MsgCreateNewList     = "Используйте /new <Название> для создания нового списка"
	MsgUndoConfirmFormat = "Вы уверены, что хотите восстановить %d удалённых элементов текущего списка? Будут восстановлены только элементы, созданные вами."
	MsgUndoCancelled     = "Восстановление отменено"
	MsgUndoAllSuccess    = "Все удалённые элементы восстановлены"
)

// escapeMarkdown escapes special characters for Markdown parsing
func escapeMarkdown(text string) string {
	specialChars := []string{"-", "*", "_", "`", "[", "]", "(", ")", ".", "!", "#", "<", ">", "{", "}", "=", "+"}
	for _, char := range specialChars {
		text = strings.ReplaceAll(text, char, "\\"+char)
	}
	return text
}

// sendMessage sends a message with error logging
func sendMessage(ctx context.Context, b *bot.Bot, chatID int64, text string) error {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      escapeMarkdown(text),
		ParseMode: models.ParseModeMarkdown,
	})
	if err != nil {
		log.Printf("Failed to send message to %d: %v", chatID, err)
	}
	return err
}

// sendInlineKeyboard sends an inline keyboard with error logging
func sendInlineKeyboard(ctx context.Context, b *bot.Bot, chatID int64, text string, kb *models.InlineKeyboardMarkup) error {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        escapeMarkdown(text),
		ReplyMarkup: kb,
		ParseMode:   models.ParseModeMarkdown,
	})
	if err != nil {
		log.Printf("Failed to send inline keyboard to %d: %v", chatID, err)
	}
	return err
}

// answerCallback answers a callback query with error logging
func answerCallback(ctx context.Context, b *bot.Bot, update *models.Update) error {
	if update.CallbackQuery == nil {
		return nil
	}
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	})
	if err != nil {
		userID := int64(0)
		if update.CallbackQuery.Message != nil {
			userID = update.CallbackQuery.Message.Chat.ID
		}
		log.Printf("Failed to answer callback for user %d: %v", userID, err)
	}
	return err
}

// getUserID extracts user ID from update
func getUserID(update *models.Update) (int64, error) {
	if update.Message != nil {
		return update.Message.Chat.ID, nil
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return update.CallbackQuery.Message.Chat.ID, nil
	}
	return 0, fmt.Errorf("no user ID found in update")
}

// parseInt64 parses a string to int64 with error handling
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
