import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    // Exclude macOS AppleDouble sidecar files (._*) this volume creates, which
    // are not valid JS and otherwise break the run.
    exclude: ['**/node_modules/**', '**/dist/**', '**/._*'],
    include: ['src/**/*.{test,spec}.{js,jsx}'],
    // A loaded CI runner makes React renders and mocked-async state updates slower
    // than testing-library's 1 s waitFor and vitest's 5 s test defaults; those
    // limits fail tests that are only slow. See src/test-setup.js.
    testTimeout: 30000,
    setupFiles: ['src/test-setup.js'],
  },
})
