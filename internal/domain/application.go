package domain

import "time"

type ExternalApplicationStatus string

const (
	StatusPending  ExternalApplicationStatus = "PENDING"
	StatusAccepted ExternalApplicationStatus = "ACCEPTED"
	StatusRejected ExternalApplicationStatus = "REJECTED"
)

type ExternalApplication struct {
	ID                    int64
	FullName              string
	Email                 string
	Phone                 *string
	OrganisationName      string
	OrganisationURL       *string
	ProjectName           string
	ProjectTypeID         int64
	TypeName              string
	ExpectedResults       string
	IsPayed               bool
	AdditionalInformation *string
	RejectionReason       *string
	Status                ExternalApplicationStatus
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type CreateExternalApplicationInput struct {
	FullName              string  `json:"fullName"`
	Email                 string  `json:"email"`
	Phone                 *string `json:"phone"`
	OrganisationName      string  `json:"organisationName"`
	OrganisationURL       *string `json:"organisationUrl"`
	ProjectName           string  `json:"projectName"`
	TypeID                int64   `json:"typeId"`
	ExpectedResults       string  `json:"expectedResults"`
	IsPayed               bool    `json:"isPayed"`
	AdditionalInformation *string `json:"additionalInformation"`
}

type ListExternalApplicationsFilter struct {
	ActiveOnly        *bool
	Search            string
	ProjectTypeID     *int64
	SortByDateUpdated SortType
	Limit             int
	Offset            int
}

type SortType string

const (
	SortAsc  SortType = "ASC"
	SortDesc SortType = "DESC"
)

type ExternalApplicationPreview struct {
	ExternalApplicationID int64                     `json:"externalApplicationId"`
	ProjectName           string                    `json:"projectName"`
	TypeName              string                    `json:"typeName"`
	Initiator             string                    `json:"initiator"`
	OrganisationName      string                    `json:"organisationName"`
	DateUpdated           time.Time                 `json:"dateUpdated"`
	Status                ExternalApplicationStatus `json:"status"`
	RejectionMessage      *string                   `json:"rejectionMessage"`
}

type ExternalApplicationList struct {
	Count        int64                        `json:"count"`
	Applications []ExternalApplicationPreview `json:"applications"`
}
