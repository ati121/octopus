package db

import (
	"fmt"
	"reflect"

	"gorm.io/gorm"
)

// 本文件处理同一个 GORM 行为造成的一类缺陷。
//
// GORM 对带 `default:true` 的 Go bool 字段做零值顶替：INSERT 时它把 false 当成
// “调用方没有赋值”，改用 tag 里的默认值 true 落库，并且会顺手把调用者结构体里的
// 字段也改成 true（callbacks/create.go 的 field.Set）。于是“创建一条停用的记录”
// 这件事没法一步做到，必须先 Create 再把列写回去。
//
// 判断哪些列本该是 false 必须在 Create **之前**完成：Create 之后内存里的值已经被
// GORM 改成 true，不能再当判据。UPDATE 路径不受顶替影响，所以补写是可靠的。

// ResetFalseBoolColumn 把 ids 对应行的 column 列显式写回 false。
//
// 适用于批量 Create（切片）之后统一补写的场景：调用方自己在 Create 前记下哪些下标
// 本该是 false，Create 拿到自增主键后把对应 ID 传进来。若调用方随后要拿这些结构体
// 去种缓存，还得把内存中的字段一并改回 false，否则数据库对了而缓存是错的。
func ResetFalseBoolColumn(tx *gorm.DB, dest any, column string, ids []int) error {
	if tx == nil || len(ids) == 0 {
		return nil
	}
	return tx.Model(dest).Where("id IN ?", ids).Update(column, false).Error
}

// CreatePreservingFalseBools 创建单条记录，并保住那些会被 `default:true` 顶替掉的
// false 布尔列。boolColumns 的 key 是 Go 字段名，value 是数据库列名。
//
// 与 ResetFalseBoolColumn 相比，本函数把“Create 前记录、Create 后补写、还原内存
// 值”三步都包进去了，调用方拿到的结构体和数据库一致。适合逐条创建、且同一结构体有
// 多个受影响布尔列的场景（例如备份导入）。
//
// value 必须是指向 struct 的非 nil 指针，且该 struct 有整型 ID 主键字段。传入的 tx
// 上已有的 Omit/Select 只作用于 Create；补写用干净会话，只更新点名的那几列。
func CreatePreservingFalseBools(tx *gorm.DB, value any, boolColumns map[string]string) error {
	if tx == nil {
		return fmt.Errorf("tx is nil")
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("value must be a non-nil pointer to a struct")
	}
	elem := rv.Elem()

	falseFields := make(map[string]string, len(boolColumns))
	for field, column := range boolColumns {
		f := elem.FieldByName(field)
		if !f.IsValid() || f.Kind() != reflect.Bool {
			return fmt.Errorf("field %s is not a bool field of %s", field, elem.Type())
		}
		if !f.Bool() {
			falseFields[field] = column
		}
	}

	if err := tx.Create(value).Error; err != nil {
		return err
	}
	if len(falseFields) == 0 {
		return nil
	}

	updates := make(map[string]any, len(falseFields))
	for field, column := range falseFields {
		elem.FieldByName(field).SetBool(false)
		updates[column] = false
	}

	idField := elem.FieldByName("ID")
	if !idField.IsValid() || !idField.CanInt() {
		return fmt.Errorf("%s has no integer ID field", elem.Type())
	}
	// 用同类型的空实例定表名，并显式带上主键条件：直接复用带 Omit 的 tx 可能把
	// Create 阶段的会话状态带进 UPDATE。
	dest := reflect.New(elem.Type()).Interface()
	return tx.Session(&gorm.Session{NewDB: true}).
		Model(dest).
		Where("id = ?", idField.Int()).
		Updates(updates).Error
}
