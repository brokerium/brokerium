// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build enterprise

package brokerium

import _ "embed"

//go:embed ee/LICENSE
var enterpriseLicense string

func init() {
	editionFiles = append(editionFiles,
		LegalFile{Name: "LICENSE-enterprise", Title: "Commercial license", Text: enterpriseLicense})
}
