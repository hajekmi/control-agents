// These viewport profiles are deterministic engine automation. They do not
// claim coverage of Safari, physical iOS/iPadOS hardware, or native system UI.
const historyViewportProfiles = Object.freeze([
  { name: "current iPhone viewport", width: 402, height: 874, windowMode: "portrait" },
  { name: "oldest-supported iPhone viewport", width: 320, height: 568, windowMode: "portrait" },
  { name: "iPad portrait", width: 820, height: 1180, windowMode: "portrait" },
  { name: "iPad landscape", width: 1180, height: 820, windowMode: "landscape" },
  { name: "iPad Split View", width: 507, height: 1112, windowMode: "split-view" },
  { name: "iPad Stage Manager", width: 744, height: 1024, windowMode: "stage-manager" }
]);

module.exports = { historyViewportProfiles };
