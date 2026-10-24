package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/audit"
	"patiklinik/internal/httpx"
)

// resolveBillingOwner fatura kime kesilecek kuralıdır (T-4):
//  1. açıkça seçilen sahip (hayvanın sahiplerinden biri olmalı) → "explicit"
//  2. hayvanın fatura sorumlusu (is_billing) → "billing_owner"
//  3. en eski kayıtlı sahip (eşitlikte küçük kimlik) → "first_owner"
func resolveBillingOwner(r *http.Request, db DB, animalID int64, explicit *int64) (int64, string, error) {
	if explicit != nil {
		var ok bool
		if err := db.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM animal_owners WHERE animal_id=$1 AND owner_id=$2)`, animalID, *explicit).Scan(&ok); err != nil {
			return 0, "", err
		}
		if !ok {
			return 0, "", httpx.Unprocessable("not_an_owner", "invoice owner must be one of the animal's owners")
		}
		return *explicit, "explicit", nil
	}
	var ownerID int64
	var isBilling bool
	err := db.QueryRow(r.Context(), `
		SELECT owner_id, is_billing FROM animal_owners WHERE animal_id=$1
		ORDER BY is_billing DESC, since, owner_id LIMIT 1`, animalID).Scan(&ownerID, &isBilling)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", httpx.Unprocessable("no_owner", "animal has no owner to bill")
	}
	if err != nil {
		return 0, "", err
	}
	if isBilling {
		return ownerID, "billing_owner", nil
	}
	return ownerID, "first_owner", nil
}

type InvoiceLine struct {
	ID             int64  `json:"id,omitempty"`
	Description    string `json:"description"`
	Quantity       int    `json:"quantity"`
	UnitPriceKurus int64  `json:"unit_price_kurus"`
	LineTotalKurus int64  `json:"line_total_kurus"`
}

type Payment struct {
	ID          int64     `json:"id"`
	AmountKurus int64     `json:"amount_kurus"`
	Method      string    `json:"method"`
	CreatedAt   time.Time `json:"created_at"`
}

type Invoice struct {
	ID          int64         `json:"id"`
	AnimalID    int64         `json:"animal_id"`
	AnimalName  string        `json:"animal_name"`
	OwnerID     int64         `json:"owner_id"`
	OwnerName   string        `json:"owner_name"`
	BillingRule string        `json:"billing_rule"`
	Status      string        `json:"status"`
	TotalKurus  int64         `json:"total_kurus"`
	PaidKurus   int64         `json:"paid_kurus"`
	DueKurus    int64         `json:"due_kurus"`
	CreatedAt   time.Time     `json:"created_at"`
	Lines       []InvoiceLine `json:"lines,omitempty"`
	Payments    []Payment     `json:"payments,omitempty"`
}

const invSelect = `
	SELECT i.id, i.animal_id, an.name, i.owner_id, o.full_name, i.billing_rule, i.status, i.total_kurus, i.paid_kurus, i.created_at
	FROM invoices i JOIN animals an ON an.id = i.animal_id JOIN owners o ON o.id = i.owner_id `

func scanInvoice(row pgx.CollectableRow) (Invoice, error) {
	var x Invoice
	err := row.Scan(&x.ID, &x.AnimalID, &x.AnimalName, &x.OwnerID, &x.OwnerName, &x.BillingRule, &x.Status, &x.TotalKurus, &x.PaidKurus, &x.CreatedAt)
	x.DueKurus = x.TotalKurus - x.PaidKurus
	return x, err
}

func loadInvoice(r *http.Request, db DB, invID int64) (Invoice, error) {
	rows, err := db.Query(r.Context(), invSelect+` WHERE i.id=$1`, invID)
	if err != nil {
		return Invoice{}, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanInvoice)
	if err != nil {
		return x, err
	}
	lrows, err := db.Query(r.Context(), `SELECT id, description, quantity, unit_price_kurus, line_total_kurus FROM invoice_lines WHERE invoice_id=$1 ORDER BY id`, invID)
	if err != nil {
		return x, err
	}
	if x.Lines, err = pgx.CollectRows(lrows, func(row pgx.CollectableRow) (InvoiceLine, error) {
		var l InvoiceLine
		return l, row.Scan(&l.ID, &l.Description, &l.Quantity, &l.UnitPriceKurus, &l.LineTotalKurus)
	}); err != nil {
		return x, err
	}
	prows, err := db.Query(r.Context(), `SELECT id, amount_kurus, method, created_at FROM payments WHERE invoice_id=$1 ORDER BY id`, invID)
	if err != nil {
		return x, err
	}
	x.Payments, err = pgx.CollectRows(prows, func(row pgx.CollectableRow) (Payment, error) {
		var p Payment
		return p, row.Scan(&p.ID, &p.AmountKurus, &p.Method, &p.CreatedAt)
	})
	if x.Payments == nil {
		x.Payments = []Payment{}
	}
	return x, err
}

type invoiceReq struct {
	AnimalID int64         `json:"animal_id"`
	OwnerID  *int64        `json:"owner_id"`
	Lines    []InvoiceLine `json:"lines"`
}

func validateLines(lines []InvoiceLine) error {
	v := httpx.Validation{}
	if len(lines) == 0 || len(lines) > 50 {
		v.Add("lines", "1-50 lines required")
	}
	for i, l := range lines {
		if len(trim(l.Description)) < 2 {
			v.Add(fmt.Sprintf("lines[%d].description", i), "required")
		}
		if l.Quantity < 1 || l.Quantity > 1000 {
			v.Add(fmt.Sprintf("lines[%d].quantity", i), "1-1000")
		}
		if l.UnitPriceKurus < 0 || l.UnitPriceKurus > 100_000_000 {
			v.Add(fmt.Sprintf("lines[%d].unit_price_kurus", i), "0-100000000 (kuruş)")
		}
	}
	return v.Err()
}

// insertInvoice faturayı ve satırlarını yazar; toplam satırlardan sunucuda hesaplanır.
func insertInvoice(r *http.Request, tx pgx.Tx, animalID, ownerID int64, rule string, lines []InvoiceLine) (int64, error) {
	var total int64
	for _, l := range lines {
		total += int64(l.Quantity) * l.UnitPriceKurus
	}
	var invID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO invoices (animal_id, owner_id, billing_rule, total_kurus, created_by, status)
		VALUES ($1,$2,$3,$4::bigint,$5, CASE WHEN $4::bigint = 0 THEN 'paid' ELSE 'open' END) RETURNING id`,
		animalID, ownerID, rule, total, principal(r).UserID).Scan(&invID); err != nil {
		return 0, err
	}
	for _, l := range lines {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price_kurus, line_total_kurus) VALUES ($1,$2,$3,$4,$5)`,
			invID, trim(l.Description), l.Quantity, l.UnitPriceKurus, int64(l.Quantity)*l.UnitPriceKurus); err != nil {
			return 0, err
		}
	}
	return invID, audit.Write(r.Context(), tx, audit.Entry{Action: "invoice.create", Entity: "invoice", EntityID: invID,
		New: map[string]any{"animal_id": animalID, "owner_id": ownerID, "billing_rule": rule, "total_kurus": total}})
}

func (a *API) createInvoice(w http.ResponseWriter, r *http.Request) error {
	var req invoiceReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := validateLines(req.Lines); err != nil {
		return err
	}
	var out Invoice
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		ownerID, rule, err := resolveBillingOwner(r, tx, req.AnimalID, req.OwnerID)
		if err != nil {
			return err
		}
		invID, err := insertInvoice(r, tx, req.AnimalID, ownerID, rule, req.Lines)
		if err != nil {
			return err
		}
		out, err = loadInvoice(r, tx, invID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (a *API) listInvoices(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	p := principal(r)
	status := r.URL.Query().Get("status")
	if status != "" && !oneOf(status, "open", "paid", "void") {
		return httpx.Validation{"status": "open, paid or void"}.Err()
	}
	ownerFilter := int64(0)
	if isOwner(p) {
		ownerFilter = ownerIDOf(p)
	}
	rows, err := a.Pool.Query(r.Context(), invSelect+`
		WHERE ($1 = '' OR i.status = $1) AND ($2 = 0 OR i.owner_id = $2) AND ($3 = 0 OR i.id < $3)
		ORDER BY i.id DESC LIMIT $4`, status, ownerFilter, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanInvoice)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(x Invoice) int64 { return x.ID }))
	return nil
}

func (a *API) getInvoice(w http.ResponseWriter, r *http.Request) error {
	invID, err := id(r)
	if err != nil {
		return err
	}
	x, err := loadInvoice(r, a.Pool, invID)
	if err != nil {
		return err
	}
	if err := ensureOwnerAccess(principal(r), x.OwnerID); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, x)
	return nil
}

type paymentReq struct {
	AmountKurus int64  `json:"amount_kurus"`
	Method      string `json:"method"`
}

// payInvoice tahsilat alır. Fatura satırı FOR UPDATE ile kilitlenir; eşzamanlı iki ödeme sırayla değerlendirilir ve
// toplamı aşan ödeme reddedilir. CHECK (paid_kurus <= total_kurus) ikinci savunma hattıdır.
func (a *API) payInvoice(w http.ResponseWriter, r *http.Request) error {
	invID, err := id(r)
	if err != nil {
		return err
	}
	var req paymentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if req.AmountKurus <= 0 {
		v.Add("amount_kurus", "must be positive (kuruş)")
	}
	if !oneOf(req.Method, "cash", "card", "transfer") {
		v.Add("method", "cash, card or transfer")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var out Invoice
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var total, paid int64
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT total_kurus, paid_kurus, status FROM invoices WHERE id=$1 FOR UPDATE`, invID).
			Scan(&total, &paid, &status); err != nil {
			return err
		}
		if status != "open" {
			return httpx.Conflict("invoice_not_open", "invoice is "+status)
		}
		if paid+req.AmountKurus > total {
			return httpx.Unprocessable("overpayment", fmt.Sprintf("payment exceeds the remaining balance of %d kuruş", total-paid)).
				With("due_kurus", total-paid)
		}
		var pid int64
		if err := tx.QueryRow(r.Context(), `INSERT INTO payments (invoice_id, amount_kurus, method, received_by) VALUES ($1,$2,$3,$4) RETURNING id`,
			invID, req.AmountKurus, req.Method, principal(r).UserID).Scan(&pid); err != nil {
			return err
		}
		newStatus := "open"
		if paid+req.AmountKurus == total {
			newStatus = "paid"
		}
		if _, err := tx.Exec(r.Context(), `UPDATE invoices SET paid_kurus = paid_kurus + $2, status = $3 WHERE id=$1`, invID, req.AmountKurus, newStatus); err != nil {
			return err
		}
		if err := audit.Write(r.Context(), tx, audit.Entry{Action: "invoice.payment", Entity: "invoice", EntityID: invID,
			Old: map[string]any{"paid_kurus": paid, "status": status},
			New: map[string]any{"paid_kurus": paid + req.AmountKurus, "status": newStatus, "payment_id": pid, "method": req.Method}}); err != nil {
			return err
		}
		out, err = loadInvoice(r, tx, invID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

// ---------------------------------------------------------------------------
// Paketler

type Package struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Species    string `json:"species"`
	Sessions   int    `json:"sessions"`
	PriceKurus int64  `json:"price_kurus"`
}

func (a *API) listPackages(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Pool.Query(r.Context(), `SELECT id, name, species, sessions, price_kurus FROM packages ORDER BY species, name`)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Package, error) {
		var x Package
		return x, row.Scan(&x.ID, &x.Name, &x.Species, &x.Sessions, &x.PriceKurus)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type purchaseReq struct {
	PackageID int64  `json:"package_id"`
	AnimalID  int64  `json:"animal_id"`
	OwnerID   *int64 `json:"owner_id"`
}

func (a *API) purchasePackage(w http.ResponseWriter, r *http.Request) error {
	var req purchaseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var purchaseID, invID int64
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var pkg Package
		if err := tx.QueryRow(r.Context(), `SELECT id, name, species, sessions, price_kurus FROM packages WHERE id=$1`, req.PackageID).
			Scan(&pkg.ID, &pkg.Name, &pkg.Species, &pkg.Sessions, &pkg.PriceKurus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("package")
			}
			return err
		}
		var species string
		if err := tx.QueryRow(r.Context(), `SELECT species FROM animals WHERE id=$1 AND merged_into IS NULL`, req.AnimalID).Scan(&species); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("animal")
			}
			return err
		}
		if species != pkg.Species {
			return httpx.Unprocessable("package_species_mismatch", "package is for "+pkg.Species)
		}
		ownerID, rule, err := resolveBillingOwner(r, tx, req.AnimalID, req.OwnerID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO package_purchases (package_id, animal_id, owner_id, sessions_total, sessions_left) VALUES ($1,$2,$3,$4,$4) RETURNING id`,
			pkg.ID, req.AnimalID, ownerID, pkg.Sessions).Scan(&purchaseID); err != nil {
			return err
		}
		invID, err = insertInvoice(r, tx, req.AnimalID, ownerID, rule, []InvoiceLine{{Description: "Paket: " + pkg.Name, Quantity: 1, UnitPriceKurus: pkg.PriceKurus}})
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "package.purchase", Entity: "package_purchase", EntityID: purchaseID,
			New: map[string]any{"package_id": pkg.ID, "animal_id": req.AnimalID, "owner_id": ownerID, "sessions": pkg.Sessions, "invoice_id": invID}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"purchase_id": purchaseID, "invoice_id": invID})
	return nil
}

type PackagePurchase struct {
	ID            int64     `json:"id"`
	PackageName   string    `json:"package_name"`
	OwnerID       int64     `json:"owner_id"`
	SessionsTotal int       `json:"sessions_total"`
	SessionsLeft  int       `json:"sessions_left"`
	Redeemed      int       `json:"redeemed"`
	PurchasedAt   time.Time `json:"purchased_at"`
}

func (a *API) listAnimalPackages(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), aid); err != nil {
		return err
	}
	rows, err := a.Pool.Query(r.Context(), `
		SELECT pp.id, p.name, pp.owner_id, pp.sessions_total, pp.sessions_left,
		       (SELECT count(*) FROM package_redemptions pr WHERE pr.purchase_id = pp.id), pp.purchased_at
		FROM package_purchases pp JOIN packages p ON p.id = pp.package_id
		WHERE pp.animal_id = $1 ORDER BY pp.purchased_at, pp.id`, aid)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PackagePurchase, error) {
		var x PackagePurchase
		return x, row.Scan(&x.ID, &x.PackageName, &x.OwnerID, &x.SessionsTotal, &x.SessionsLeft, &x.Redeemed, &x.PurchasedAt)
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []PackagePurchase{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}
