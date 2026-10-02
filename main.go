package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"work-schedule/internal/services"
	"work-schedule/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	st, err := store.Open("data/work-schedule.db")
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	app := application.New(application.Options{
		Name:        "排班工具",
		Description: "教师排班系统",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Services: []application.Service{
			application.NewService(services.NewScopeService(st)),
			application.NewService(services.NewMetadataService(st)),
			application.NewService(services.NewCalendarService(st)),
			application.NewService(services.NewScheduleService(st)),
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "排班工具",
		Width:  1280,
		Height: 800,
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
