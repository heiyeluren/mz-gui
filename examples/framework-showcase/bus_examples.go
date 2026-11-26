package main

import (
	"context"
	"fmt"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/commandbus"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/querybus"
)

func probeShowcaseBusPatterns(ctx context.Context) error {
	commands := commandbus.New()
	if err := commands.Register("showcase.ping", func(context.Context, commandbus.Command) (commandbus.Result, error) {
		return commandbus.Result{Value: "pong"}, nil
	}); err != nil {
		return err
	}
	commandResult, err := commands.Dispatch(ctx, commandbus.Command{Type: "showcase.ping", Meta: showcaseNoticeMeta("command")})
	if err != nil {
		return err
	}
	if commandResult.Value != "pong" {
		return fmt.Errorf("showcase CommandBus 返回无效: %#v", commandResult.Value)
	}

	queries := querybus.New()
	if err := queries.Register("showcase.health", func(context.Context, querybus.Query) (querybus.Result, error) {
		return querybus.Result{Value: "ready"}, nil
	}); err != nil {
		return err
	}
	queryResult, err := queries.Ask(ctx, querybus.Query{Type: "showcase.health", Meta: showcaseNoticeMeta("query")})
	if err != nil {
		return err
	}
	if queryResult.Value != "ready" {
		return fmt.Errorf("showcase QueryBus 返回无效: %#v", queryResult.Value)
	}
	return nil
}
