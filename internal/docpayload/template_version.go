package docpayload

// TemplateDocumentV1 is the template version issuing writes into
// FrozenPayloadV1.TemplateVersion. It must equal
// internal/pdf.CurrentTemplateVersion: the renderer (issue #74) prints a
// payload with the template its TemplateVersion names and keeps every
// version it ever shipped. The constant lives here until the renderer
// merges, so issuing (issue #70) does not import internal/pdf.
const TemplateDocumentV1 = "document-v1"
