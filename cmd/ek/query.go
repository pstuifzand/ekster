package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

func parseQueryOptions(args []string) (microsub.ItemSearchOptions, error) {
	o := microsub.ItemSearchOptions{Channel: "global"}
	positional := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional++
			switch positional {
			case 1:
				o.Query = arg
			case 2:
				o.Channel = arg
			default:
				return o, fmt.Errorf("too many query arguments")
			}
			continue
		}
		i++
		if i == len(args) {
			return o, fmt.Errorf("missing value for %s", arg)
		}
		value := args[i]
		switch arg {
		case "-author":
			o.Author = append(o.Author, value)
		case "-category":
			o.Category = append(o.Category, value)
		case "-kind":
			o.Kind = append(o.Kind, value)
		case "-source":
			o.Source = append(o.Source, value)
		case "-before":
			o.Before = value
		case "-after":
			o.After = value
		case "-limit":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return o, fmt.Errorf("limit must be positive")
			}
			o.Limit = n
		case "-channel":
			o.Channel = value
		default:
			return o, fmt.Errorf("unknown query option %s", arg)
		}
	}
	return o, microsub.ValidateItemSearchOptions(o)
}
