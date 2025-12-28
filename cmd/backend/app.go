package main

import (
	"time"

	"gihub.com/kayon/memscan"
	"gihub.com/kayon/memscan/deck"
	"gihub.com/kayon/memscan/scanner"
)

const (
	MinResultsThreshold = 10
	MaxResultsThreshold = 50
)

type App struct {
	scan                   *memscan.Memscan
	game                   *deck.Process
	value                  *scanner.Value
	renderResultsThreshold int
}

func (app *App) SetRenderResultsThreshold(value int) {
	if value >= MinResultsThreshold && value <= MaxResultsThreshold && value != app.renderResultsThreshold {
		app.renderResultsThreshold = value
	}
}

func (app *App) Clear() {
	app.scan.Reset()
	app.ResetScan()
}

func (app *App) ResetScan() {
	app.scan.Reset()
	app.value = nil
}

func (app *App) GameProcess(appID int64) *deck.Process {
	if app.game == nil || (app.game.AppID != appID || !app.game.Alive()) {
		app.game = deck.FindGameWithAppID(appID)
	}
	return app.game
}

// FirstScan 在此之前调用 GameProcess
// 在UI中保存进程信息用于调试, 其它任何时候不再返回 Process
func (app *App) FirstScan(appID int64, value string, valueType scanner.Type, option scanner.Option) *Results {
	app.GameProcess(appID)
	if app.game == nil {
		return nil
	}
	app.scan.Reset()
	err := app.scan.Open(app.game)
	if err != nil {
		return nil
	}

	app.value = parseValue(value, valueType)

	if app.value == nil {
		return nil
	}

	app.value.WithOption(option)
	dur := app.scan.FirstScan(app.value)
	return app.render(dur)
}

func (app *App) NextScan(value string) *Results {
	if app.game == nil || app.value == nil {
		return nil
	}
	if app.scan.Rounds() < 1 {
		return nil
	}
	if app.scan.Count() == 0 {
		return app.render(0)
	}

	option := app.value.Option()
	app.value = parseValue(value, app.value.Type())
	if app.value == nil {
		return app.render(0)
	}
	app.value.WithOption(option)

	dur := app.scan.NextScan(app.value)
	return app.render(dur)
}

func (app *App) ChangeValues(value string, indexes []int) *Results {
	if app.game == nil || app.value == nil {
		return nil
	}
	if app.scan.Count() == 0 {
		return nil
	}

	app.value = parseValue(value, app.value.Type())
	if app.value == nil {
		return nil
	}

	app.scan.ChangeResultsValues(indexes, app.value)
	return app.render(0)
}

func (app *App) RefreshValues() *Results {
	if app.game == nil || app.value == nil {
		return nil
	}
	if app.scan.Count() == 0 || app.scan.Count() > app.renderResultsThreshold {
		return nil
	}
	return app.render(0)
}

func (app *App) render(dur time.Duration) *Results {
	results := &Results{
		Count: app.scan.Count(),
		Round: app.scan.Rounds(),
	}
	if dur > 0 {
		results.Time = dur.String()
	}
	if results.Count <= app.renderResultsThreshold {
		results.List = app.scan.RenderResults(app.value)
	}
	return results
}
