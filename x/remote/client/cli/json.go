package cli

import "encoding/json"

func unmarshalList(raw []byte, out *[]map[string]interface{}) error { return json.Unmarshal(raw, out) }

func marshalItem(item map[string]interface{}) ([]byte, error) { return json.Marshal(item) }
