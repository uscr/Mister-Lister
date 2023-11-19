package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/go-telegram/ui/keyboard/inline"
)

func listKeyboard(b *bot.Bot, userId int64) (*inline.Keyboard, error) {
	db, err := getDb()
	if err != nil {
		return nil, err
	}

	var listOwners []ListOwners
	db.Where(&ListOwners{UserID: userId}).Find(&listOwners)

	kb := inline.New(b).Row()
	for _, element := range listOwners {
		var list List
		db.First(&list, "id = ?", element.ListID)
		kb.Button(list.Name, []byte(list.Name), onListSelect)
		kb.Row()
	}

	return kb, nil
}

func onListSelect(ctx context.Context, b *bot.Bot, mes *models.Message, data []byte) {
	userId := mes.Chat.ID
	listName := string(data)
	selectListByName(userId, listName)
	selectedList, err := getSelectedList(userId)
	if err != nil {
		sendMessage(ctx, b, userId, fmt.Sprintf("Ошибка выбора активного списка"))
	}
	kb, err := listItemsKeyboard(b, selectedList)
	sendMarkupKeyboard(ctx, b, userId, fmt.Sprintf("Список %s:", selectedList.Name), kb)
}

func listHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID
	text := "/new *Название* : создать новый список"
	sendMessage(ctx, b, update.Message.Chat.ID, text)
	kb, err := listKeyboard(b, userId)
	if err != nil {
		sendMessage(ctx, b, userId, "Ошибка формирования меню списка")
		return
	}
	sendInlineKeyboard(ctx, b, userId, fmt.Sprintf("Выберите список:"), kb)
}

func newListHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	var text string
	userId := update.Message.Chat.ID
	words := strings.Fields(update.Message.Text)
	if len(words) < 2 {
		helpHandler(ctx, b, update)
		return
	}
	name := strings.Join(words[1:], " ")

	if err := createList(userId, name); err != nil {
		errorLog.Printf("[listHandler] User %d. Error when create list: %s", userId, err)
		text = "Ошибка создания списка"
	} else {
		text = fmt.Sprintf("Создали %s и сделали его активным", name)
	}

	sendMessage(ctx, b, userId, text)

}
