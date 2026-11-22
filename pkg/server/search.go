package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

func (h *microsubHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	_, content := r.Form["channel"]
	if !content {
		feeds, err := h.backend.Search(r.Context(), query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		respondJSON(w, map[string][]microsub.Feed{"results": feeds})
		return
	}
	o := microsub.ItemSearchOptions{Channel: r.FormValue("channel"), Query: query, Author: append(r.Form["author"], r.Form["author[]"]...), Category: append(r.Form["category"], r.Form["category[]"]...), Kind: append(r.Form["kind"], r.Form["kind[]"]...), Source: append(r.Form["source"], r.Form["source[]"]...), Before: r.FormValue("before"), After: r.FormValue("after")}
	if raw, ok := r.Form["limit"]; ok {
		if len(raw) != 1 {
			http.Error(w, "limit must be a single positive integer", http.StatusBadRequest)
			return
		}
		n, err := strconv.Atoi(r.FormValue("limit"))
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a single positive integer", http.StatusBadRequest)
			return
		}
		o.Limit = n
	}
	if err := microsub.ValidateItemSearchOptions(o); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.backend.ItemSearchWithOptions(r.Context(), o)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, microsub.ErrInvalidItemSearch) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	if result.Items == nil {
		result.Items = []microsub.Item{}
	}
	respondJSON(w, result)
}
