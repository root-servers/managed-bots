package macrobot

import (
	"database/sql"

	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
	"github.com/keybase/managed-bots/base"
)

type DB struct {
	*base.DB
}

func NewDB(db *sql.DB) *DB {
	return &DB{
		DB: base.NewDB(db),
	}
}

func (d *DB) Create(name string, convID chat1.ConvIDStr, isConv bool, macroName, macroMessage string) (created bool, err error) {
	err = d.RunTxn(func(tx *sql.Tx) error {
		if isConv {
			name = string(convID)
		}
		res, err := tx.Exec(`
			INSERT INTO macro
			(channel_name, is_conv, macro_name, macro_message)
			VALUES
			(?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
			macro_message=VALUES(macro_message)
		`, name, isConv, macroName, macroMessage)
		if err != nil {
			return err
		}
		numRows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		// https://dev.mysql.com/doc/refman/5.7/en/insert-on-duplicate.html
		created = numRows == 1
		return nil
	})
	return created, err
}

func (d *DB) Get(name string, convID chat1.ConvIDStr, macroName string) (message string, err error) {
	row := d.DB.QueryRow(`
		SELECT macro_message
		FROM macro
		WHERE (channel_name = ? OR channel_name = ?) AND macro_name = ?
		-- prefer is_conv=true
		ORDER BY is_conv DESC
		LIMIT 1
	`, name, convID, macroName)
	err = row.Scan(&message)
	return message, err
}

type Macro struct {
	Name    string
	Message string
	IsConv  bool
}

func (d *DB) List(name string, convID chat1.ConvIDStr) (list []Macro, err error) {
	rows, err := d.DB.Query(`
		SELECT macro_name, macro_message, is_conv
		FROM macro
		WHERE channel_name = ?
		OR channel_name = ?
		-- prefer is_conv=true
		ORDER BY macro_name ASC, is_conv DESC
	`, name, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var macro Macro
		if err := rows.Scan(&macro.Name, &macro.Message, &macro.IsConv); err != nil {
			return nil, err
		}
		list = append(list, macro)
	}
	return list, nil
}

func (d *DB) Remove(name string, convID chat1.ConvIDStr, macroName string) (removed bool, err error) {
	err = d.RunTxn(func(tx *sql.Tx) error {
		// First try to delete for the conv
		res, err := tx.Exec(`
			DELETE FROM macro
			WHERE channel_name = ? AND macro_name = ?
		`, convID, macroName)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		} else if rows == 1 {
			removed = true
			return nil
		}
		// Now try teamwide
		res, err = tx.Exec(`
			DELETE FROM macro
			WHERE channel_name = ? AND macro_name = ?
		`, name, macroName)
		if err != nil {
			return err
		}
		rows, err = res.RowsAffected()
		removed = rows == 1
		return err
	})
	return removed, err
}
