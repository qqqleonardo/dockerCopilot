package module

import (
	"encoding/json"
	"net/http"
	"sync"
)

func syncMapReset() sync.Map {
	return sync.Map{}
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
