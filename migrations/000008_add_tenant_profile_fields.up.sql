ALTER TABLE tenants
    ADD COLUMN description TEXT,
    ADD COLUMN contact_email VARCHAR(255),
    ADD COLUMN contact_phone VARCHAR(20),
    ADD COLUMN timezone VARCHAR(64);
