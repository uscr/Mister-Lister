package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type List struct {
	gorm.Model
	ID   int64  `gorm:"primaryKey"`
	Name string `gorm:"not null"`
}

type Settings struct {
	ID           int64
	UserID       int64 `gorm:"primaryKey"`
	SelectedList int64
	List         List `gorm:"foreignKey:SelectedList"`
}

type ListOwners struct {
	gorm.Model
	UserID int64 `gorm:"index"`
	ListID int64
	List   List `gorm:"foreignKey:ListID"`
}

type ListItem struct {
	gorm.Model
	ID         int64  `gorm:"primaryKey" json:"id"`
	UserID     int64  `gorm:"index" json:"user_id"`
	Name       string `gorm:"not null" json:"name"`
	ListID     int64  `json:"list_id"`
	List       List   `gorm:"foreignKey:ListID" json:"list"`
	Item_order int    `gorm:"default:0" json:"item_order"`
}

func initDb() error {
	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Migrate the schema
	if err := db.AutoMigrate(&List{}, &ListItem{}, &Settings{}, &ListOwners{}); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	return nil
}

func getDb() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(os.Getenv("MISTER_LISTER_SQLITE_DB")), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return db, nil
}

func getSelectedList(ctx context.Context, userID int64) (List, error) {
	db, err := getDb()
	if err != nil {
		return List{}, fmt.Errorf("failed to get database: %w", err)
	}

	var settings Settings
	if err := db.WithContext(ctx).First(&settings, "user_id = ?", userID).Error; err != nil {
		return List{}, fmt.Errorf("failed to get settings for user %d: %w", userID, err)
	}

	if settings.SelectedList == 0 {
		return List{}, fmt.Errorf("no selected list for user %d", userID)
	}

	var list List
	if err := db.WithContext(ctx).First(&list, "id = ?", settings.SelectedList).Error; err != nil {
		return List{}, fmt.Errorf("failed to get list %d for user %d: %w", settings.SelectedList, userID, err)
	}

	return list, nil
}

func addItem(ctx context.Context, userID int64, itemName string) error {
	if itemName == "" {
		return errors.New("item name cannot be empty")
	}

	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	list, err := getSelectedList(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get selected list for user %d: %w", userID, err)
	}

	// Find the maximum item_order value for the list to append the new item at the end
	var maxOrder struct{ Item_order int }
	db.WithContext(ctx).Model(&ListItem{}).Select("COALESCE(MAX(item_order), 0) as item_order").
		Where("list_id = ?", list.ID).Scan(&maxOrder)

	item := ListItem{
		UserID:     userID,
		ListID:     list.ID,
		Name:       itemName,
		Item_order: maxOrder.Item_order + 1,
	}
	if err := db.WithContext(ctx).Create(&item).Error; err != nil {
		return fmt.Errorf("failed to create item '%s' for user %d: %w", itemName, userID, err)
	}
	return nil
}

func selectListByName(ctx context.Context, userID int64, listName string) error {
	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	var list List
	if err := db.WithContext(ctx).Where("name = ?", listName).First(&list).Error; err != nil {
		return fmt.Errorf("list '%s' not found for user %d: %w", listName, userID, err)
	}

	// Check if user is an owner of the list
	var owner ListOwners
	if err := db.WithContext(ctx).Where("user_id = ? AND list_id = ?", userID, list.ID).First(&owner).Error; err != nil {
		return fmt.Errorf("user %d is not an owner of list '%s': %w", userID, listName, err)
	}

	if err := db.WithContext(ctx).Save(&Settings{
		UserID:       userID,
		SelectedList: list.ID,
	}).Error; err != nil {
		return fmt.Errorf("failed to update settings for user %d: %w", userID, err)
	}
	return nil
}

func createList(ctx context.Context, userID int64, listName string) error {
	if listName == "" {
		return errors.New("list name cannot be empty")
	}

	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	// Check for existing list with the same name for the user
	var existingList List
	if err := db.WithContext(ctx).Where("name = ?", listName).First(&existingList).Error; err == nil {
		var owner ListOwners
		if err := db.WithContext(ctx).Where("user_id = ? AND list_id = ?", userID, existingList.ID).First(&owner).Error; err == nil {
			return fmt.Errorf("list '%s' already exists for user %d", listName, userID)
		}
	}

	list := List{Name: listName}
	if err := db.WithContext(ctx).Create(&list).Error; err != nil {
		return fmt.Errorf("failed to create list '%s' for user %d: %w", listName, userID, err)
	}

	if err := db.WithContext(ctx).Save(&Settings{
		UserID:       userID,
		SelectedList: list.ID,
	}).Error; err != nil {
		return fmt.Errorf("failed to update settings for user %d: %w", userID, err)
	}

	if err := addOwner(ctx, userID, list.ID); err != nil {
		return fmt.Errorf("failed to add owner for list %d, user %d: %w", list.ID, userID, err)
	}
	return nil
}

func addOwner(ctx context.Context, userID int64, listID int64) error {
	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	// Check if user is already an owner
	var existingOwner ListOwners
	if err := db.WithContext(ctx).Where("user_id = ? AND list_id = ?", userID, listID).First(&existingOwner).Error; err == nil {
		return nil // User is already an owner
	}

	owner := ListOwners{UserID: userID, ListID: listID}
	if err := db.WithContext(ctx).Create(&owner).Error; err != nil {
		return fmt.Errorf("failed to add owner %d to list %d: %w", userID, listID, err)
	}
	return nil
}

func deleteListElement(ctx context.Context, userID int64, listID int64, elementID int64) error {
	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	var owner ListOwners
	if err := db.WithContext(ctx).Where("user_id = ? AND list_id = ?", userID, listID).First(&owner).Error; err != nil {
		return fmt.Errorf("user %d is not an owner of list %d: %w", userID, listID, err)
	}

	if err := db.WithContext(ctx).
		Where("id = ? AND list_id = ?", elementID, listID).
		Delete(&ListItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete item %d from list %d for user %d: %w", elementID, listID, userID, err)
	}
	return nil
}

func getListItems(ctx context.Context, userID, listID int64) ([]ListItem, error) {
	db, err := getDb()
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}

	var items []ListItem
	if err := db.WithContext(ctx).
		Where("list_id = ?", listID).
		Order("item_order ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch items for list %d: %w", listID, err)
	}

	return items, nil
}

func reorderListItems(ctx context.Context, userID int64, listID int64, itemIDs []int64) error {
	db, err := getDb()
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	var owner ListOwners
	if err := db.WithContext(ctx).Where("user_id = ? AND list_id = ?", userID, listID).First(&owner).Error; err != nil {
		return fmt.Errorf("user %d is not an owner of list %d: %w", userID, listID, err)
	}

	tx := db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, itemID := range itemIDs {
		var item ListItem
		if err := tx.WithContext(ctx).Where("id = ? AND list_id = ?", itemID, listID).First(&item).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("item %d does not exist in list %d: %w", itemID, listID, err)
		}
	}

	for i, itemID := range itemIDs {
		if err := tx.Model(&ListItem{}).
			Where("id = ? AND list_id = ?", itemID, listID).
			Update("item_order", i+1).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update item_order for item %d in list %d: %w", itemID, listID, err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
