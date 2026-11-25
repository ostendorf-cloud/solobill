package docpayload

// TemplateDocumentV1 is the first template version. A payload frozen
// with it keeps naming it, and internal/pdf keeps rendering it.
const TemplateDocumentV1 = "document-v1"

// TemplateCurrent is the template version issuing writes into
// FrozenPayloadV1.TemplateVersion. It must equal
// internal/pdf.CurrentTemplateVersion (internal/pdf's tests compare
// them): the renderer prints a payload with the template its
// TemplateVersion names and keeps every version it ever shipped. It
// lives here so issuing does not import internal/pdf. document-v2 is
// document-v1 plus the seller logo, with the same cells and limits.
const TemplateCurrent = "document-v2"
