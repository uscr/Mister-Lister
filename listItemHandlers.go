package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func listItemsKeyboard(b *bot.Bot, list List) (*models.InlineKeyboardMarkup, error) {
	db, err := getDb()
	if err != nil {
		return nil, err
	}

	var listItem []ListItem
	db.Order("created_at").Find(&listItem, "list_id = ?", list.ID)

	// Двумерный массив
	kb := &models.InlineKeyboardMarkup{InlineKeyboard: make([][]models.InlineKeyboardButton, 0)}
	maxLineLength := 30
	maxButtonsOnRow := 6

	lineLength := 0                                                                       // Общее количество символов на кнопках в строке
	lineCursor := 0                                                                       // Номер строки которую сейчас собираем
	rowCursor := 0                                                                        // Номер кнопки в строке которую добавили
	kb.InlineKeyboard = append(kb.InlineKeyboard, make([]models.InlineKeyboardButton, 0)) // Это потом мы отправим в API

	// Здесь будем собирать строку с кнопками
	// до выполнения условия при котором нужно будет перейти на следующую строку
	InlineKeyboardRow := []models.InlineKeyboardButton{}
	// В цикле собираем строки скливиатуры с элементами списка
	for _, element := range listItem {
		// Если в строке много кнопок или количество симоволов в кнопках на строке выше лимита
		if rowCursor == maxButtonsOnRow || lineLength+len(element.Name) >= maxLineLength {
			// Принимаем строку которую насобирали к отправке в API
			kb.InlineKeyboard[lineCursor] = append(kb.InlineKeyboard[lineCursor], InlineKeyboardRow...)
			// Увеличиваем счётчик строк
			lineCursor++
			// Добавляем пустую строку
			kb.InlineKeyboard = append(kb.InlineKeyboard, make([]models.InlineKeyboardButton, 0))

			// Опустошаем строку, обнуляем счётчики
			InlineKeyboardRow = []models.InlineKeyboardButton{}
			rowCursor = 0
			lineLength = 0
		}
		// Добавляем элемент
		InlineKeyboardRow = append(InlineKeyboardRow, models.InlineKeyboardButton{Text: element.Name, CallbackData: fmt.Sprintf("deleteListElement_%d_%d", list.ID, element.ID)})
		// Передвигаем "курсор" и суммируем длинну символов в строке
		rowCursor++
		lineLength += len(element.Name)

	}
	// Последняя строка может не добавиться, если if внутри цикла не выполнится
	// Добавим её
	if len(InlineKeyboardRow) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, make([]models.InlineKeyboardButton, 0))
		kb.InlineKeyboard[lineCursor+1] = append(kb.InlineKeyboard[lineCursor], InlineKeyboardRow...)
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{{Text: "Вернуть удалённое", CallbackData: "UndoDeleteListElement"}})
	return kb, nil
}

func drawListItemsHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	selectedList, err := getSelectedList(update.Message.Chat.ID)
	if err != nil {
		sendMessage(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Ошибка выбора активного списка"))
	}
	kb, err := listItemsKeyboard(b, selectedList)
	sendMarkupKeyboard(ctx, b, update.Message.Chat.ID, fmt.Sprintf("%s:", selectedList.Name), kb)
}

func onListUndoDelete(ctx context.Context, b *bot.Bot, update *models.Update) {
	answerCallback(ctx, b, update)
	userId := update.CallbackQuery.Message.Chat.ID
	db, err := getDb()
	if err != nil {
		errorLog.Println(err)
		return
	}
	list, _ := getSelectedList(userId)
	var lastDeletedItem ListItem
	db.Unscoped().Order("deleted_at desc").First(&lastDeletedItem, "user_id = ? AND list_id = ? AND deleted_at NOT NULL", userId, list.ID)
	db.Unscoped().Model(&lastDeletedItem).Update("deleted_at", nil)
	buyList, _ := getSelectedList(userId)
	kb, err := listItemsKeyboard(b, buyList)
	if err != nil {
		sendMessage(ctx, b, userId, "Ошибка формирования меню списка")
		return
	}
	sendMarkupKeyboard(ctx, b, userId, fmt.Sprintf("Список: %s", list.Name), kb)
}

func onListElementClick(ctx context.Context, b *bot.Bot, update *models.Update) {
	answerCallback(ctx, b, update)
	userId := update.CallbackQuery.Message.Chat.ID
	buttonData := strings.Split(update.CallbackQuery.Data, "_")

	listId, _ := strconv.ParseInt(buttonData[1], 0, 64)
	listElementId, _ := strconv.ParseInt(buttonData[2], 0, 64)
	deleteListElement(userId, listId, listElementId)
	buyList, err := getSelectedList(userId)
	if err != nil {
		sendMessage(ctx, b, userId, "Ошибка получения активного списка")
		return
	}
	kb, err := listItemsKeyboard(b, buyList)
	if err != nil {
		sendMessage(ctx, b, userId, "Ошибка формирования меню списка")
		return
	}

	sendMarkupKeyboard(ctx, b, userId, fmt.Sprintf("Список: %s", buyList.Name), kb)

}
