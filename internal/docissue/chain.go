package docissue

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// RootOf is the id of the chain d belongs to: its own id for an invoice,
// the original it corrects for a correction. An offer belongs to no
// chain.
func RootOf(d storage.Document) (uuid.UUID, bool) {
	switch d.Type {
	case storage.DocumentTypeInvoice:
		return d.ID, true
	case storage.DocumentTypeCorrection:
		if d.CorrectsDocumentID != nil {
			return *d.CorrectsDocumentID, true
		}
	}

	return uuid.Nil, false
}

// member is d as a chain member: the original is sequence 0, a correction
// its CorrectionSeq.
func member(d storage.Document) (docchain.Member, bool) {
	if d.State != storage.DocumentStateIssued {
		return docchain.Member{}, false
	}
	m := docchain.Member{ID: d.ID}
	if d.IssuedAt != nil {
		m.IssuedOn = IssueDate(*d.IssuedAt)
	}
	switch d.Type {
	case storage.DocumentTypeInvoice:
		return m, true
	case storage.DocumentTypeCorrection:
		if d.CorrectionSeq == nil {
			return docchain.Member{}, false
		}
		m.Seq = int(*d.CorrectionSeq)

		return m, true
	}

	return docchain.Member{}, false
}

// ChainOf builds the chain of the issued documents of one chain, as
// storage.DocumentRepository.Chain returns them.
func ChainOf(documents []storage.Document) (docchain.Chain, error) {
	members := make([]docchain.Member, 0, len(documents))
	for _, d := range documents {
		if m, ok := member(d); ok {
			members = append(members, m)
		}
	}
	chain, err := docchain.New(members)
	if err != nil {
		return docchain.Chain{}, fmt.Errorf("docissue: %w", err)
	}

	return chain, nil
}

// Statuses derives the chain status of every issued invoice and
// correction in documents: effective for the newest issued document of
// its chain, superseded for every older one. Drafts, offers and members
// of a chain whose original is not in documents get none. The status is
// derived when reading and never written onto an issued row.
func Statuses(documents []storage.Document) map[uuid.UUID]docchain.Status {
	byRoot := map[uuid.UUID][]docchain.Member{}
	for _, d := range documents {
		root, ok := RootOf(d)
		if !ok {
			continue
		}
		if m, ok := member(d); ok {
			byRoot[root] = append(byRoot[root], m)
		}
	}

	out := map[uuid.UUID]docchain.Status{}
	for _, members := range byRoot {
		chain, err := docchain.New(members)
		if err != nil {
			continue
		}
		for _, m := range members {
			if status, ok := chain.StatusOf(m.ID); ok {
				out[m.ID] = status
			}
		}
	}

	return out
}
