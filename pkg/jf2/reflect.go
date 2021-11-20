/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package jf2

import (
	"reflect"

	"p83.nl/go/ekster/pkg/microsub"
	"willnorris.com/go/microformats"
)

func convertItemProps(item interface{}, props map[string][]interface{}) {
	sv := reflect.ValueOf(item).Elem()
	st := reflect.TypeOf(item).Elem()

	for i := 0; i < st.NumField(); i++ {
		ft := st.Field(i)
		fv := sv.Field(i)

		if value, ok := ft.Tag.Lookup("mf2"); ok {
			if value == "" {
				continue
			}
			if s, e := props[value]; e {
				if len(s) > 0 {
					if str, ok := s[0].(string); ft.Type.Kind() == reflect.String && ok {
						fv.SetString(str)
					} else if ft.Type.Kind() == reflect.Slice {
						for _, v := range s {
							fv.Set(reflect.Append(fv, reflect.ValueOf(v)))
						}
					} else if card, ok := s[0].(map[string]interface{}); ok {
						var hcard microsub.Card
						if t, ok := card["type"].([]interface{}); ok {
							hcard.Type = t[0].(string)[2:]
						}
						if properties, ok := card["properties"].(map[string]interface{}); ok {
							ps := make(map[string][]interface{})
							for k, v := range properties {
								ps[k] = v.([]interface{})
							}
							convertItemProps(&hcard, ps)
						}
						fv.Set(reflect.ValueOf(&hcard))
					}
				}
			}
		}
	}
}

// ConvertItem converts items based on struct tags
func ConvertItem(item interface{}, md *microformats.Microformat) {
	sv := reflect.ValueOf(item).Elem()
	sv.FieldByName("Type").SetString(md.Type[0][2:])
	convertItemProps(item, md.Properties)
}
