package server

import (
	"context"
	"database/sql"
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) invoiceEvent(ctx context.Context, uid int64, kind string, ref *store.Invoice) core.CommitHook {
	if !s.core.Configured() {
		return nil
	}
	u, err := s.store.GetUser(ctx, uid)
	return func(tx *sql.Tx) error {
		if err != nil {
			return err
		}
		inv, err := store.InvoiceSnapshot(ctx, tx, uid, ref.ID)
		if err != nil {
			return err
		}
		topic := "invoicing.invoice_" + kind
		if kind == "status" {
			if inv.Status != store.StatusSent && inv.Status != store.StatusPaid {
				return nil
			}
			topic = "invoicing.invoice_" + inv.Status
		}
		if kind == "paid" && inv.Status != store.StatusPaid {
			return nil
		}
		return core.QueueEvent(ctx, tx, topic, fmt.Sprintf("invoice:%d", inv.ID), map[string]any{"invoice_id": inv.ID, "number": inv.Number, "client_id": inv.ClientID, "client": inv.ClientName, "status": inv.Status, "issue_date": inv.IssueDate, "due_date": inv.DueDate, "total": inv.Total, "amount_paid": inv.AmountPaid, "currency": inv.Currency, "user_email": u.Email})
	}
}
