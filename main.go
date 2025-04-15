package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AddressBrasil struct {
	Cep          string `json:"cep"`
	State        string `json:"state"`
	City         string `json:"city"`
	Neighborhood string `json:"neighborhood"`
	Street       string `json:"street"`
	Service      string `json:"-"`
}

type AddressViaCep struct {
	Cep        string `json:"cep"`
	Uf         string `json:"uf"`
	Localidade string `json:"localidade"`
	Bairro     string `json:"bairro"`
	Logradouro string `json:"logradouro"`
	Service    string `json:"-"`
}

type resultadoAPI struct {
	Origem string      `json:"origem"`
	Data   interface{} `json:"data"`
	Err    error       `json:"erro,omitempty"`
}

func fetchFromBrasilAPI(ctx context.Context, cep string) (AddressBrasil, error) {
	start := time.Now()
	url := fmt.Sprintf("https://brasilapi.com.br/api/cep/v1/%s", cep)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		err := fmt.Errorf("error creating request: %v", err)
		return AddressBrasil{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return AddressBrasil{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return AddressBrasil{}, fmt.Errorf("requisição falhou: %s", resp.Status)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return AddressBrasil{}, fmt.Errorf("error reading response: %v", err)
	}
	var address AddressBrasil
	if err := json.Unmarshal(body, &address); err != nil {
		return AddressBrasil{}, fmt.Errorf("error reading response: %v", err)
	}

	duration := time.Since(start)
	fmt.Println("Tempo de resposta BrasilAPI:", duration)
	return address, nil
}

func fetchFromViaCep(ctx context.Context, cep string) (AddressViaCep, error) {
	start := time.Now()
	url := fmt.Sprintf("https://viacep.com.br/ws/%s/json/", cep)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		err := fmt.Errorf("error creating request: %v", err)
		return AddressViaCep{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return AddressViaCep{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return AddressViaCep{}, fmt.Errorf("error reading response: %v", err)
	}
	var address AddressViaCep
	if err := json.Unmarshal(body, &address); err != nil {
		return AddressViaCep{}, fmt.Errorf("error reading response: %v", err)
	}
	duration := time.Since(start)
	fmt.Println("Tempo de resposta ViaCep:", duration)
	return address, nil

}

func handleCEP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 || parts[2] == "" {
		http.Error(w, "Uso correto: /cep/{cep}", http.StatusBadRequest)
		return
	}
	cep := parts[2]
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	resChan := make(chan resultadoAPI, 2)
	go func() {
		brasil, errBrasil := fetchFromBrasilAPI(ctx, cep)
		if errBrasil != nil {
			resChan <- resultadoAPI{Origem: "brasilapi", Data: nil, Err: errBrasil}
			return
		}
		resChan <- resultadoAPI{Origem: "brasilapi", Data: brasil}
	}()
	go func() {
		viacep, errVia := fetchFromViaCep(ctx, cep)
		if errVia != nil {
			resChan <- resultadoAPI{Origem: "viacep", Data: nil, Err: errVia}
			return
		}
		resChan <- resultadoAPI{Origem: "viacep", Data: viacep}
	}()

	result := <-resChan
	cancel()
	if result.Err != nil {
		if errors.Is(result.Err, context.DeadlineExceeded) {
			http.Error(w, "Erro: tempo de espera excedido", http.StatusRequestTimeout)
			return
		}
		http.Error(w, "Erro: "+result.Err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch result.Origem {
	case "viacep":
		if data, ok := result.Data.(AddressViaCep); ok {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resultadoAPI{
				Origem: "viacep",
				Data:   data,
			})
			return
		}
	case "brasilapi":
		if data, ok := result.Data.(AddressBrasil); ok {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resultadoAPI{
				Origem: "brasilapi",
				Data:   data,
			})
			return
		}
	default:
		http.Error(w, "Erro interno: tipo inesperado de resposta", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/cep/", handleCEP)
	http.ListenAndServe(":8080", nil)
}
