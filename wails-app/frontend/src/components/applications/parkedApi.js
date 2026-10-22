import * as WailsApp from '../../wailsjs/go/main/App'

// The parked-application bindings (app_application_park.go). Each call starts
// inside a promise, so a missing binding or bridge lands in the caller's catch.
export const parkedApi = {
  tab: (id) => Promise.resolve().then(() => WailsApp.GetApplicationTab(id)),
  confirm: (id) => Promise.resolve().then(() => WailsApp.ConfirmApplicationSubmitted(id)),
  resume: (id) => Promise.resolve().then(() => WailsApp.ResumeApplicationFill(id)),
  sweep: () => Promise.resolve().then(() => WailsApp.SweepApplicationTabs()),
  queue: (ids, maxParked = 0) => Promise.resolve().then(() => WailsApp.StartApplicationQueue(ids, maxParked)),
}
