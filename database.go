package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var dbLogger = logger.New(
	log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
	logger.Config{
		SlowThreshold:             time.Second, // Slow SQL threshold
		LogLevel:                  logger.Info, // Log level
		IgnoreRecordNotFoundError: false,       // Ignore ErrRecordNotFound error for logger
		ParameterizedQueries:      false,       // Don't include params in the SQL log
		Colorful:                  false,       // Disable color
	},
)

type List struct {
	gorm.Model
	ID   int64 `gorm:"primaryKey"`
	Name string
}

type Settings struct {
	ID           int64
	UserID       int64 `gorm:"primaryKey"`
	SelectedList int64
	List         List `gorm:"foreignKey:SelectedList"`
}

type ListOwners struct {
	gorm.Model
	ID     int64 `gorm:"primaryKey"`
	UserID int64 `gorm:"index"`
	ListID int64
	List   List
}

type ListItem struct {
	gorm.Model
	ID     int64 `gorm:"primaryKey"`
	UserID int64 `gorm:"index"`
	Name   string
	ListID int64
	List   List
}

func initDb() error {
	fmt.Println(os.Getenv("MISTER_LISTER_SQLITE_DB"))
	db, err := getDb()
	if err != nil {
		return err
	}
	db.AutoMigrate(&List{}, &ListItem{}, &Settings{}, &ListOwners{})
	return nil
}

func getDb() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(os.Getenv("MISTER_LISTER_SQLITE_DB")), &gorm.Config{})
	// db, err := gorm.Open(sqlite.Open(os.Getenv("MISTER_LISTER_SQLITE_DB")), &gorm.Config{
	// 	Logger: dbLogger,
	// })
	return db, err
}

func getSelectedList(UserID int64) (List, error) {
	db, err := getDb()
	if err != nil {
		return List{}, err
	}
	var selectedListSettings Settings
	db.First(&selectedListSettings, "user_id = ?", UserID)
	var selectedlist List
	db.First(&selectedlist, "id = ?", selectedListSettings.SelectedList)
	return selectedlist, nil
}

func addItem(userId int64, item string) error {
	db, err := getDb()
	if err != nil {
		return err
	}
	selectedlist, err := getSelectedList(userId)
	if err != nil {
		return err
	}
	if selectedlist.ID == 0 {
		return errors.New("[addItem] List not found")
	}
	newitem := ListItem{UserID: userId, List: selectedlist, Name: item}
	db.Create(&newitem)
	return nil
}

func selectListByName(userId int64, listName string) error {
	db, err := getDb()
	if err != nil {
		return err
	}
	var newSelectedList List
	result := db.First(&newSelectedList, "name = ?", listName)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return errors.New(fmt.Sprintf("[selectListByName] list name %s for user %d not found", listName, userId))
	}
	if result.Error != nil {
		return result.Error
	}
	db.Save(&Settings{UserID: userId, SelectedList: newSelectedList.ID})
	return nil
}

func createList(userId int64, listName string) error {
	db, err := getDb()
	if err != nil {
		return err
	}
	newlist := List{Name: listName}
	db.Create(&newlist)
	db.Save(&Settings{UserID: userId, SelectedList: newlist.ID})
	err = addOwner(userId, newlist.ID)
	return err
}

func addOwner(userId int64, listId int64) error {
	db, err := getDb()
	if err != nil {
		return err
	}
	result := db.Save(&ListOwners{UserID: userId, ListID: listId})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func deleteListElement(userId int64, listID int64, elementId int64) error {
	db, err := getDb()
	if err != nil {
		return err
	}
	db.Delete(&ListItem{}, elementId)
	return nil
}
