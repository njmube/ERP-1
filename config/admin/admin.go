package admin

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	//"reflect"

	"golang.org/x/crypto/bcrypt"

	"github.com/jinzhu/gorm"
	"github.com/qor/action_bar"
	"github.com/qor/activity"
	"github.com/qor/admin"
	"github.com/qor/help"
	//"github.com/qor/i18n/exchange_actions"
	"github.com/qor/media"
	"github.com/qor/media/asset_manager"
	"github.com/qor/media/media_library"
	"github.com/qor/notification"
	"github.com/qor/notification/channels/database"
	"github.com/qor/publish2"
	"github.com/qor/qor"
	"github.com/reechou/erp/app/models"
	"github.com/reechou/erp/config/admin/bindatafs"
	"github.com/reechou/erp/config/auth"
	"github.com/reechou/erp/config/i18n"
	"github.com/reechou/erp/db"
	"github.com/qor/qor/resource"
	//"github.com/qor/qor/utils"
	//"github.com/qor/transition"
	"github.com/qor/validations"
)

var Admin *admin.Admin
var ActionBar *action_bar.ActionBar
var Countries = []string{"China", "Japan", "USA"}
var OrderStates = []string{"已付款", "处理中", "已配送", "已退货"}
var Agencies = []string{"一件代发", "批量拿货"}

func init() {
	Admin = admin.New(&qor.Config{DB: db.DB.Set(publish2.VisibleMode, publish2.ModeOff).Set(publish2.ScheduleMode, publish2.ModeOff)})
	Admin.SetSiteName("XERP")
	Admin.SetAuth(auth.AdminAuth{})
	Admin.SetAssetFS(bindatafs.AssetFS)

	// Add Asset Manager, for rich editor
	assetManager := Admin.AddResource(&asset_manager.AssetManager{}, &admin.Config{Invisible: true})

	// Add Help
	Help := Admin.NewResource(&help.QorHelpEntry{})
	Help.GetMeta("Body").Config = &admin.RichEditorConfig{AssetManager: assetManager}

	// Add Notification
	Notification := notification.New(&notification.Config{})
	Notification.RegisterChannel(database.New(&database.Config{DB: db.DB}))
	Notification.Action(&notification.Action{
		Name: "Confirm",
		Visible: func(data *notification.QorNotification, context *admin.Context) bool {
			return data.ResolvedAt == nil
		},
		MessageTypes: []string{"order_returned"},
		Handle: func(argument *notification.ActionArgument) error {
			orderID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(argument.Message.Body)[1]
			err := argument.Context.GetDB().Model(&models.Order{}).Where("id = ? AND returned_at IS NULL", orderID).Update("returned_at", time.Now()).Error
			if err == nil {
				return argument.Context.GetDB().Model(argument.Message).Update("resolved_at", time.Now()).Error
			}
			return err
		},
		Undo: func(argument *notification.ActionArgument) error {
			orderID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(argument.Message.Body)[1]
			err := argument.Context.GetDB().Model(&models.Order{}).Where("id = ? AND returned_at IS NOT NULL", orderID).Update("returned_at", nil).Error
			if err == nil {
				return argument.Context.GetDB().Model(argument.Message).Update("resolved_at", nil).Error
			}
			return err
		},
	})
	Notification.Action(&notification.Action{
		Name:         "Check it out",
		MessageTypes: []string{"order_paid_cancelled", "order_processed", "order_returned"},
		URL: func(data *notification.QorNotification, context *admin.Context) string {
			return path.Join("/admin/orders/", regexp.MustCompile(`#(\d+)`).FindStringSubmatch(data.Body)[1])
		},
	})
	Notification.Action(&notification.Action{
		Name:         "Dismiss",
		MessageTypes: []string{"order_paid_cancelled", "info", "order_processed", "order_returned"},
		Visible: func(data *notification.QorNotification, context *admin.Context) bool {
			return data.ResolvedAt == nil
		},
		Handle: func(argument *notification.ActionArgument) error {
			return argument.Context.GetDB().Model(argument.Message).Update("resolved_at", time.Now()).Error
		},
		Undo: func(argument *notification.ActionArgument) error {
			return argument.Context.GetDB().Model(argument.Message).Update("resolved_at", nil).Error
		},
	})
	Admin.NewResource(Notification)

	// Add Dashboard
	Admin.AddMenu(&admin.Menu{Name: "Dashboard", Link: "/admin"})

	//* Product Management *//
	color := Admin.AddResource(&models.Color{}, &admin.Config{Menu: []string{"Product Management"}, Priority: -5})
	Admin.AddResource(&models.Size{}, &admin.Config{Menu: []string{"Product Management"}, Priority: -4})

	category := Admin.AddResource(&models.Category{}, &admin.Config{Menu: []string{"Product Management"}, Priority: -3})

	Admin.AddResource(&models.Collection{}, &admin.Config{Menu: []string{"Product Management"}, Priority: -2})

	// Add ProductImage as Media Libraray
	ProductImagesResource := Admin.AddResource(&models.ProductImage{}, &admin.Config{Menu: []string{"Product Management"}, Priority: -1})

	ProductImagesResource.Filter(&admin.Filter{
		Name:       "SelectedType",
		Label:      "Media Type",
		Operations: []string{"contains"},
		Config:     &admin.SelectOneConfig{Collection: [][]string{{"video", "Video"}, {"image", "Image"}, {"file", "File"}, {"video_link", "Video Link"}}},
	})
	ProductImagesResource.Filter(&admin.Filter{
		Name:   "Color",
		Config: &admin.SelectOneConfig{RemoteDataResource: color},
	})
	ProductImagesResource.Filter(&admin.Filter{
		Name:   "Category",
		Config: &admin.SelectOneConfig{RemoteDataResource: category},
	})
	ProductImagesResource.IndexAttrs("File", "Title")

	// Add Product
	product := Admin.AddResource(&models.Product{}, &admin.Config{Menu: []string{"Product Management"}})
	product.Meta(&admin.Meta{Name: "MadeCountry", Config: &admin.SelectOneConfig{Collection: Countries}})
	product.Meta(&admin.Meta{Name: "Description", Config: &admin.RichEditorConfig{AssetManager: assetManager, Plugins: []admin.RedactorPlugin{
		{Name: "medialibrary", Source: "/admin/assets/javascripts/qor_redactor_medialibrary.js"},
		{Name: "table", Source: "/javascripts/redactor_table.js"},
	},
		Settings: map[string]interface{}{
			"medialibraryUrl": "/admin/product_images",
		},
	}})
	product.Meta(&admin.Meta{Name: "Category", Config: &admin.SelectOneConfig{AllowBlank: true}})
	product.Meta(&admin.Meta{Name: "Collections", Config: &admin.SelectManyConfig{SelectMode: "bottom_sheet"}})

	product.Meta(&admin.Meta{Name: "MainImage", Config: &media_library.MediaBoxConfig{
		RemoteDataResource: ProductImagesResource,
		Max:                1,
		Sizes: map[string]*media.Size{
			"main": {Width: 300, Height: 300},
		},
	}})
	product.Meta(&admin.Meta{Name: "MainImageURL", Valuer: func(record interface{}, context *qor.Context) interface{} {
		if p, ok := record.(*models.Product); ok {
			result := bytes.NewBufferString("")
			tmpl, _ := template.New("").Parse("<img src='{{.image}}'></img>")
			tmpl.Execute(result, map[string]string{"image": p.MainImageURL()})
			return template.HTML(result.String())
		}
		return ""
	}})

	product.UseTheme("grid")

	colorVariationMeta := product.Meta(&admin.Meta{Name: "ColorVariations"})
	colorVariationMeta.SetFormattedValuer(func(record interface{}, context *qor.Context) interface{} {
		colorValue := colorVariationMeta.GetValuer()(record, context).([]models.ColorVariation)
		var results []string
		for _, v := range colorValue {
			results = append(results, v.ColorSizeInfo())
		}
		return results
	})
	
	colorVariation := colorVariationMeta.Resource
	colorVariation.Meta(&admin.Meta{Name: "Images", Config: &media_library.MediaBoxConfig{
		RemoteDataResource: ProductImagesResource,
		Sizes: map[string]*media.Size{
			"icon":    {Width: 50, Height: 50},
			"preview": {Width: 300, Height: 300},
			"listing": {Width: 640, Height: 640},
		},
	}})

	colorVariation.NewAttrs("-Product", "-ColorCode")
	colorVariation.EditAttrs("-Product", "-ColorCode")
	
	sizeVariationMeta := colorVariation.Meta(&admin.Meta{Name: "SizeVariations"})
	sizeVariation := sizeVariationMeta.Resource
	sizeVariation.EditAttrs(
		&admin.Section{
			Rows: [][]string{
				{"Size", "AvailableQuantity"},
				//{"ShareableVersion"},
			},
		},
	)
	sizeVariation.NewAttrs(sizeVariation.EditAttrs())

	product.SearchAttrs("Name", "Code", "Category.Name", "Brand.Name")
	product.IndexAttrs("MainImageURL", "Name", "Price", "ColorVariations")
	product.EditAttrs(
		//&admin.Section{
		//	Title: "Seo Meta",
		//	Rows: [][]string{
		//		{"Seo"},
		//	}},
		&admin.Section{
			Title: "Basic Information",
			Rows: [][]string{
				{"Name"},
				{"Code", "Price"},
				{"MainImage"},
			}},
		&admin.Section{
			Title: "Organization",
			Rows: [][]string{
				{"Category", "MadeCountry"},
				//{"Collections"},
			}},
		//"ProductProperties",
		//"Description",
		"ColorVariations",
	)
	product.NewAttrs(product.EditAttrs())
	
	product.Filter(&admin.Filter{
		Name:   "Category",
		Config: &admin.SelectOneConfig{RemoteDataResource: category},
	})

	for _, country := range Countries {
		var country = country
		product.Scope(&admin.Scope{Name: country, Group: "Made Country", Handle: func(db *gorm.DB, ctx *qor.Context) *gorm.DB {
			return db.Where("made_country = ?", country)
		}})
	}

	product.Action(&admin.Action{
		Name: "View On Site",
		URL: func(record interface{}, context *admin.Context) string {
			if product, ok := record.(*models.Product); ok {
				return fmt.Sprintf("/products/%v", product.Code)
			}
			return "#"
		},
		Modes: []string{"menu_item", "edit"},
	})
	
	// Add Customer
	customer := Admin.AddResource(&models.Customer{}, &admin.Config{Menu: []string{"User Management"}, Priority: -2})
	customer.IndexAttrs("ID", "Name", "Wechat", "Phone")
	customer.ShowAttrs(
		&admin.Section{
			Title: "Basic Information",
			Rows: [][]string{
				{"Name"},
				{"Wechat", "Phone"},
			}},
	)
	customer.NewAttrs(customer.ShowAttrs())
	customer.EditAttrs(customer.ShowAttrs())
	
	// Add User
	user := Admin.AddResource(&models.User{}, &admin.Config{Menu: []string{"User Management"}, Priority: -1})
	user.Meta(&admin.Meta{Name: "Gender", Config: &admin.SelectOneConfig{Collection: []string{"男", "女", "未知"}}})
	user.Meta(&admin.Meta{Name: "Role", Config: &admin.SelectOneConfig{Collection: []string{ "用户", "管理员", "维护员"}}})
	user.Meta(&admin.Meta{Name: "Password",
		Type:            "password",
		FormattedValuer: func(interface{}, *qor.Context) interface{} { return "" },
		Setter: func(resource interface{}, metaValue *resource.MetaValue, context *qor.Context) {
			values := metaValue.Value.([]string)
			if len(values) > 0 {
				if newPassword := values[0]; newPassword != "" {
					bcryptPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
					if err != nil {
						context.DB.AddError(validations.NewError(user, "Password", "Can't encrpt password"))
						return
					}
					u := resource.(*models.User)
					u.Password = string(bcryptPassword)
				}
			}
		},
	})
	user.Meta(&admin.Meta{Name: "Confirmed", Valuer: func(user interface{}, ctx *qor.Context) interface{} {
		if user.(*models.User).ID == 0 {
			return true
		}
		return user.(*models.User).Confirmed
	}})
	
	user.Filter(&admin.Filter{
		Name: "Role",
		Config: &admin.SelectOneConfig{
			Collection: []string{"管理员", "维护员", "用户"},
		},
	})
	
	user.IndexAttrs("ID", "Email", "Name", "Gender", "Role")
	user.ShowAttrs(
		&admin.Section{
			Title: "Basic Information",
			Rows: [][]string{
				{"Name"},
				{"Email", "Password"},
				{"Gender", "Role"},
				{"Confirmed"},
			}},
		"Addresses",
	)
	user.EditAttrs(user.ShowAttrs())

	// Add Order
	order := Admin.AddResource(&models.Order{}, &admin.Config{Menu: []string{"Order Management"}})
	//order.Meta(&admin.Meta{
	//	Name:   "Customer",
	//	Config: &admin.SelectOneConfig{SelectMode: "bottom_sheet", RemoteDataResource: customer},
	//})
	order.Meta(&admin.Meta{Name: "State", Config: &admin.SelectOneConfig{Collection: OrderStates}})
	order.Meta(&admin.Meta{Name: "ShippingAddress", Type: "single_edit"})
	order.Meta(&admin.Meta{Name: "ShippedAt", Type: "date"})

	orderItemMeta := order.Meta(&admin.Meta{Name: "OrderItems"})
	orderItemMeta.Resource.Meta(&admin.Meta{Name: "SizeVariation", Config: &admin.SelectOneConfig{Collection: sizeVariationCollection}})
	orderItemMeta.Resource.Meta(&admin.Meta{Name: "State", Config: &admin.SelectOneConfig{Collection: OrderStates}})
	orderItemMeta.SetFormattedValuer(func(record interface{}, context *qor.Context) interface{} {
		itemValue := orderItemMeta.GetValuer()(record, context).([]models.OrderItem)
		db := context.GetDB()
		var results []string
		for _, v := range itemValue {
			db.First(&v.SizeVariation, v.SizeVariationID)
			db.Model(&v.SizeVariation).Related(&v.SizeVariation.ColorVariation)
			db.Model(&v.SizeVariation).Related(&v.SizeVariation.Size)
			db.Model(&v.SizeVariation.ColorVariation).Related(&v.SizeVariation.ColorVariation.Product)
			db.Model(&v.SizeVariation.ColorVariation).Related(&v.SizeVariation.ColorVariation.Color)
			results = append(results, fmt.Sprintf("[%s#%s#%s#%d]", v.SizeVariation.ColorVariation.Product.Name, v.SizeVariation.ColorVariation.Color.Name, v.SizeVariation.Size.Name, v.Quantity))
		}
		return results
	})

	// define scopes for Order
	for _, state := range OrderStates {
		var state = state
		order.Scope(&admin.Scope{
			Name:  state,
			Label: strings.Title(strings.Replace(state, "_", " ", -1)),
			Group: "Order Status",
			Handle: func(db *gorm.DB, context *qor.Context) *gorm.DB {
				return db.Where(models.Order{State: state})
			},
		})
	}

	// define actions for Order
	type trackingNumberArgument struct {
		TrackingNumber string
	}

	order.Action(&admin.Action{
		Name: "Processing",
		Handle: func(argument *admin.ActionArgument) error {
			for _, record := range argument.FindSelectedRecords() {
				db := argument.Context.GetDB()
				//if err := models.OrderState.Trigger("process", order.(*models.Order), db); err != nil {
				//	return err
				//}
				order := record.(*models.Order)
				//fmt.Println(order)
				//db.Table("order_items").Where("order_id = ?", order.ID).Updates(map[string]interface{}{"state": OrderStates[1]})
				//db.Model(order).UpdateColumn("state", OrderStates[1])
				order.State = OrderStates[1]
				db.Select("state").Save(order)
			}
			return nil
		},
		Visible: func(record interface{}, context *admin.Context) bool {
			return true
			if order, ok := record.(*models.Order); ok {
				return order.State == "paid"
			}
			return false
		},
		Modes: []string{"show", "menu_item"},
	})
	order.Action(&admin.Action{
		Name: "Ship",
		Handle: func(argument *admin.ActionArgument) error {
			var (
				tx                     = argument.Context.GetDB().Begin()
				trackingNumberArgument = argument.Argument.(*trackingNumberArgument)
			)

			if trackingNumberArgument.TrackingNumber != "" {
				for _, record := range argument.FindSelectedRecords() {
					order := record.(*models.Order)
					order.State = OrderStates[2]
					order.TrackingNumber = &trackingNumberArgument.TrackingNumber
					//models.OrderState.Trigger("ship", order, tx, "tracking number "+trackingNumberArgument.TrackingNumber)
					if err := tx.Save(order).Error; err != nil {
						tx.Rollback()
						return err
					}
				}
			} else {
				return errors.New("invalid shipment number")
			}

			tx.Commit()
			return nil
		},
		Visible: func(record interface{}, context *admin.Context) bool {
			return true
			if order, ok := record.(*models.Order); ok {
				return order.State == "processing"
			}
			return false
		},
		Resource: Admin.NewResource(&trackingNumberArgument{}),
		Modes:    []string{"show", "menu_item"},
	})

	order.Action(&admin.Action{
		Name: "Return",
		Handle: func(argument *admin.ActionArgument) error {
			for _, order := range argument.FindSelectedRecords() {
				db := argument.Context.GetDB()
				//if err := models.OrderState.Trigger("cancel", order.(*models.Order), db); err != nil {
				//	return err
				//}
				order.(*models.Order).State = OrderStates[3]
				db.Select("state").Save(order)
			}
			return nil
		},
		Visible: func(record interface{}, context *admin.Context) bool {
			return true
			if order, ok := record.(*models.Order); ok {
				for _, state := range []string{"draft", "checkout", "paid", "processing"} {
					if order.State == state {
						return true
					}
				}
			}
			return false
		},
		Modes: []string{"index", "show", "menu_item"},
	})

	order.IndexAttrs("Customer", "PaymentAmount", "OrderItems", "TrackingNumber", "State", "ShippingAddress")
	order.NewAttrs("-DiscountValue", "-AbandonedReason")
	order.EditAttrs("-DiscountValue", "-AbandonedReason", "-State")
	order.ShowAttrs("-DiscountValue", "-State")
	order.SearchAttrs("Customer.Name", "Customer.Wechat", "ShippingAddress.ContactName", "ShippingAddress.Address1", "ShippingAddress.Address2")

	// Add activity for order
	activity.Register(order)
	
	// add agency
	agency := Admin.AddResource(&models.Agency{}, &admin.Config{Menu: []string{"Agency Management"}, Priority: -3})
	agency.Meta(&admin.Meta{Name: "AgencyItem", Config: &admin.SelectOneConfig{Collection: agencyCollection}})
	agency.IndexAttrs("Customer", "AgencyItem", "Balance")
	agency.SearchAttrs("Customer.Name")
	
	agencyItem := Admin.AddResource(&models.AgencyItem{}, &admin.Config{Menu: []string{"Agency Management"}, Priority: -2})
	agencyItem.Meta(&admin.Meta{Name: "Type", Config: &admin.SelectOneConfig{Collection: Agencies}})
	
	agencyLog := Admin.AddResource(&models.AgencyLog{}, &admin.Config{Menu: []string{"Agency Management"}, Priority: -1})
	agencyLog.Filter(&admin.Filter{
		Name: "Opr",
		Config: &admin.SelectOneConfig{
			Collection: models.AGENCY_OPR_ORDER,
		},
	})
	agencyLog.SearchAttrs("Customer.Name")

	// Add Store
	//store := Admin.AddResource(&models.Store{}, &admin.Config{Menu: []string{"Store Management"}})
	//store.Meta(&admin.Meta{Name: "Owner", Type: "single_edit"})
	//store.AddValidator(func(record interface{}, metaValues *resource.MetaValues, context *qor.Context) error {
	//	if meta := metaValues.Get("Name"); meta != nil {
	//		if name := utils.ToString(meta.Value); strings.TrimSpace(name) == "" {
	//			return validations.NewError(record, "Name", "Name can't be blank")
	//		}
	//	}
	//	return nil
	//})

	// Blog Management
	//article := Admin.AddResource(&models.Article{}, &admin.Config{Menu: []string{"Blog Management"}})
	//article.IndexAttrs("ID", "VersionName", "ScheduledStartAt", "ScheduledEndAt", "Author", "Title")

	// Add Translations
	Admin.AddResource(i18n.I18n, &admin.Config{Menu: []string{"Site Management"}, Priority: 1})

	// Add Worker
	//Worker := getWorker()
	//exchange_actions.RegisterExchangeJobs(i18n.I18n, Worker)
	//Admin.AddResource(Worker, &admin.Config{Menu: []string{"Site Management"}})

	// Add Setting
	Admin.AddResource(&models.Setting{}, &admin.Config{Name: "Shop Setting", Singleton: true})

	// Add Search Center Resources
	Admin.AddSearchResource(product, customer, order, agency)

	// Add ActionBar
	ActionBar = action_bar.New(Admin)
	ActionBar.RegisterAction(&action_bar.Action{Name: "Admin Dashboard", Link: "/admin"})

	initWidgets()
	initSeo()
	initFuncMap()
	initRouter()
}

func sizeVariationCollection(resource interface{}, context *qor.Context) (results [][]string) {
	for _, sizeVariation := range models.SizeVariations() {
		results = append(results, []string{strconv.Itoa(int(sizeVariation.ID)), sizeVariation.Stringify()})
	}
	return
}

func agencyCollection(resource interface{}, context *qor.Context) (results [][]string) {
	for _, v := range models.AgencyItems() {
		results = append(results, []string{strconv.Itoa(int(v.ID)), v.Stringify()})
	}
	return
}
