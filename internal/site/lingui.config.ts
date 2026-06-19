import { defineConfig } from "@lingui/cli"
import { formatter } from "@lingui/format-po"

export default defineConfig({
	locales: ["en", "fr"],
	sourceLocale: "en",
	compileNamespace: "ts",
	format: formatter({ lineNumbers: false }),
	catalogs: [
		{
			path: "<rootDir>/src/locales/{locale}/{locale}",
			include: ["src"],
		},
	],
})
