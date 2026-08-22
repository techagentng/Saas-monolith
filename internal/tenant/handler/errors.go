package handler

import apperrors "github.com/techagentng/saas-monolith/internal/errors"

func applicationError(err error) apperrors.HTTPError {
	return apperrors.Map(err)
}
