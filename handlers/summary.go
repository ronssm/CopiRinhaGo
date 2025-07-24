package handlers

import (
    "encoding/json"
    "net/http"
    "CopiRinhaGo/db"
)

func HandlePaymentsSummary(w http.ResponseWriter, r *http.Request) {
    from := r.URL.Query().Get("from")
    to := r.URL.Query().Get("to")
    summary, err := db.GetPaymentsSummary(from, to)
    if err != nil {
        http.Error(w, "summary error", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(summary)
}
