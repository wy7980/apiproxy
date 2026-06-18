package auth

import "net/http"

type KeyStore struct {
	keys map[string]string // api_key -> client_id
}

func NewKeyStore(pairs [][2]string) *KeyStore {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p[0]] = p[1]
	}
	return &KeyStore{keys: m}
}

func (ks *KeyStore) Authenticate(r *http.Request) (clientID string, ok bool) {
	key := r.Header.Get("Authorization")
	if len(key) > 7 && key[:7] == "Bearer " {
		key = key[7:]
	}
	if key == "" {
		return "", false
	}
	id, found := ks.keys[key]
	return id, found
}
