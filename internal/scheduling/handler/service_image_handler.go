package handler

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// maxUploadMemory bounds how much of a multipart request body
// ParseMultipartForm buffers in memory before spilling to temp files; it is
// deliberately just above one full-size image (model.MaxImageSizeBytes) so a
// single-file upload never touches disk for buffering, while
// MaxImagesPerService files at the ceiling size do. This is a parsing detail
// only — the actual per-file and per-service limits are enforced by
// ServiceImageService, never by this constant.
const maxUploadMemory = model.MaxImageSizeBytes + 1<<20

// ServiceImageHandler serves a service's image collection.
//
// Every route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New, reusing service.read (list) and
// service.update (upload/update/reorder/delete) — the same reasoning
// CategoryService documents for reusing service.* rather than inventing a
// parallel permission family. This handler performs no permission check of
// its own and no domain validation: both belong to ServiceImageService.
type ServiceImageHandler struct {
	images service.ServiceImageService
}

func NewServiceImageHandler(images service.ServiceImageService) *ServiceImageHandler {
	return &ServiceImageHandler{images: images}
}

// PublicServiceImage is the authenticated, owner-facing image representation.
//
// storage_key is deliberately absent: the owner dashboard never needs it —
// every management action (delete, reorder, set primary) addresses an image
// by its id — so there is no reason to hand back an internal storage handle
// even to an authenticated caller. tenant_id and service_id are absent for
// the same reason PublicService omits tenant_id: both are already known from
// the route.
type PublicServiceImage struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	AltText   *string `json:"alt_text"`
	SortOrder int     `json:"sort_order"`
	IsPrimary bool    `json:"is_primary"`
}

// toPublicServiceImage is the single conversion point, so the four response
// sites below cannot drift apart on the field list.
func toPublicServiceImage(image *model.ServiceImage) PublicServiceImage {
	return PublicServiceImage{
		ID: image.ID, URL: image.PublicURL, AltText: image.AltText,
		SortOrder: image.SortOrder, IsPrimary: image.IsPrimary,
	}
}

func toPublicServiceImages(images []*model.ServiceImage) []PublicServiceImage {
	result := make([]PublicServiceImage, len(images))
	for i, image := range images {
		result[i] = toPublicServiceImage(image)
	}
	return result
}

// Create handles POST /api/v1/tenants/{tenantID}/services/{serviceID}/images.
//
// multipart/form-data, one or more files under the "images" field. The
// backend re-enforces the total-image-count and per-file limits regardless
// of anything the client believes it already checked — ServiceImageService
// never trusts this handler's parsing to have applied any real limit.
func (h *ServiceImageHandler) Create(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	if err := request.ParseMultipartForm(maxUploadMemory); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid multipart request", err))
		return
	}
	defer request.MultipartForm.RemoveAll()

	fileHeaders := request.MultipartForm.File["images"]
	if len(fileHeaders) == 0 {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "no files found in the \"images\" field", nil))
		return
	}

	inputs := make([]service.UploadImageInput, 0, len(fileHeaders))
	for _, header := range fileHeaders {
		file, err := header.Open()
		if err != nil {
			writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "could not read an uploaded file", err))
			return
		}
		defer file.Close()
		inputs = append(inputs, service.UploadImageInput{Body: file})
	}

	created, err := h.images.Upload(request.Context(), tenantID, serviceID, inputs)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, struct {
		Images []PublicServiceImage `json:"images"`
	}{Images: toPublicServiceImages(created)})
}

// List handles GET /api/v1/tenants/{tenantID}/services/{serviceID}/images.
// Returns a bare array, matching every other listing endpoint in this
// module (ServiceHandler.List, ServiceCategoryHandler.List) — an empty
// collection serializes as [] rather than null.
func (h *ServiceImageHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	images, err := h.images.List(request.Context(), tenantID, serviceID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceImages(images))
}

// Update handles PATCH /api/v1/tenants/{tenantID}/services/{serviceID}/images/{imageID}.
// The only writable fields are alt_text and is_primary — the decode target
// has no field for anything else, the same structural protection every
// other PATCH decode target in this module relies on.
func (h *ServiceImageHandler) Update(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string, imageID string) {
	var input struct {
		AltText   *string `json:"alt_text"`
		IsPrimary *bool   `json:"is_primary"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	updated, err := h.images.UpdateMeta(request.Context(), tenantID, serviceID, imageID, service.UpdateImageInput{
		AltText: input.AltText, IsPrimary: input.IsPrimary,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceImage(updated))
}

// Delete handles DELETE /api/v1/tenants/{tenantID}/services/{serviceID}/images/{imageID}.
// No body in either direction: the image is gone, there is nothing left to
// return, and 204 says so without inventing a placeholder payload.
func (h *ServiceImageHandler) Delete(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string, imageID string) {
	if err := h.images.Delete(request.Context(), tenantID, serviceID, imageID); err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// ReplaceOrder handles PUT /api/v1/tenants/{tenantID}/services/{serviceID}/images/order.
// One bulk reorder rather than one PATCH per image — the request is a single
// permutation of the service's current image ids; anything else (a missing
// id, an extra one, a duplicate, or one from another service/tenant) is
// refused by ServiceImageService before any write happens.
func (h *ServiceImageHandler) ReplaceOrder(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	var input struct {
		ImageIDs []string `json:"image_ids"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	reordered, err := h.images.Reorder(request.Context(), tenantID, serviceID, input.ImageIDs)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceImages(reordered))
}
