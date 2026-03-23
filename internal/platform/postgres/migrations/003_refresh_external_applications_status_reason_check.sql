ALTER TABLE external_applications
DROP CONSTRAINT IF EXISTS chk_external_applications_status_reason;

ALTER TABLE external_applications
ADD CONSTRAINT chk_external_applications_status_reason
CHECK (
    (status = 'PENDING' AND rejection_reason IS NULL) OR
    (status = 'ACCEPTED' AND rejection_reason IS NULL) OR
    (status = 'REJECTED' AND rejection_reason IS NOT NULL AND btrim(rejection_reason) <> '')
);
