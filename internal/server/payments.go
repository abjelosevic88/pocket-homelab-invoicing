package server

import (
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleListPayments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListPayments(r.Context(), store.PaymentFilter{InvoiceID: qInt64(r, "invoice_id"), ClientID: qInt64(r, "client_id"), From: q.Get("from"), To: q.Get("to"), Limit: qInt(r, "limit", 200)})
	if err != nil {
		s.fail(w, err, "list payments")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var p store.Payment
	if err := decode(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	inv, err := s.store.GetInvoice(ctx, p.InvoiceID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invoice not found")
		return
	}
	if p.Amount <= 0 {
		writeErr(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	if p.Date == "" {
		p.Date = store.Today()
	}
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Currency == "" {
		p.Currency = inv.Currency
	}
	if p.Method == "" {
		p.Method = "bank_transfer"
	}
	// Applied amount is in the invoice currency.
	cur := s.store.GetCurrency(ctx, inv.Currency)
	if p.Currency == inv.Currency {
		p.ExchangeRate = 1
		p.AppliedAmount = money.Round(p.Amount, cur.Decimals)
	} else {
		if p.ExchangeRate <= 0 {
			rate, ok := s.store.GetRate(ctx, p.Currency, inv.Currency)
			if !ok {
				writeErr(w, http.StatusBadRequest, "no exchange rate for "+p.Currency+"→"+inv.Currency+"; supply exchange_rate")
				return
			}
			p.ExchangeRate = rate
		}
		p.AppliedAmount = money.Convert(p.Amount, p.ExchangeRate, cur.Decimals)
	}
	if err := s.store.CreatePayment(ctx, &p); err != nil {
		s.fail(w, err, "create payment")
		return
	}
	updated, err := s.store.RecalcAmountPaid(ctx, inv.ID)
	if err != nil {
		s.fail(w, err, "recalc")
		return
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "payment", "Payment of "+money.FormatCode(p.Amount, cur.Decimals, p.Currency)+" recorded")
	s.hooks.Emit("payment.created", map[string]any{"payment": p, "invoice": updated})
	if updated.Status == store.StatusPaid {
		s.hooks.Emit("invoice.paid", updated)
		s.paperlessAutoArchive(updated)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"payment": p, "invoice": updated})
}

func (s *Server) handleDeletePayment(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := s.store.DeletePayment(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete payment")
		return
	}
	inv, err := s.store.RecalcAmountPaid(r.Context(), invoiceID)
	if err != nil {
		s.fail(w, err, "recalc")
		return
	}
	s.store.LogActivity(r.Context(), "invoice", invoiceID, "payment", "Payment deleted")
	writeJSON(w, http.StatusOK, inv)
}
