---
mytonies-app

yarn "build-e2e:ios"                    # https://app.bitrise.io/build/b5172d71-9dac-44c4-b735-29fd1e74d33b
fastlane "buildAndDeployInternal"       # https://app.bitrise.io/build/bd72f07c-c9ca-4a74-88fd-ae52fa79d9f2

---
Shopify

pnpm + rust + xcodebuild                # https://app.bitrise.io/build/6ca5a04d-6927-46fb-a09d-aba523e1714d
pnpm + rust + gradle                    # https://app.bitrise.io/build/1e1ba1a1-b662-42ec-9064-041ee4a888a2


  ---                                                                                                                                                                                
  Package Managers                                          
                                                                                                                                                                                     
  ┌───────────────────┬────────────────────────────────────────────┐
  │       Tool        │                   Notes                    │
  ├───────────────────┼────────────────────────────────────────────┤
  │ npm               │ Default, ships with Node.js                │
  ├───────────────────┼────────────────────────────────────────────┤
  │ Yarn (v1 Classic) │ Still very common in RN projects           │
  ├───────────────────┼────────────────────────────────────────────┤
  │ Yarn Berry (v4+)  │ Plug'n'Play, zero-installs                 │
  ├───────────────────┼────────────────────────────────────────────┤
  │ pnpm              │ Best for monorepos, 87% less disk than npm │
  ├───────────────────┼────────────────────────────────────────────┤
  │ Bun               │ Fastest installs, also a runtime + bundler │
  └───────────────────┴────────────────────────────────────────────┘

  ---
  JS Bundlers

  ┌─────────┬──────────────────────────────────────────┐
  │  Tool   │                  Notes                   │
  ├─────────┼──────────────────────────────────────────┤
  │ Metro   │ Default RN bundler                       │
  ├─────────┼──────────────────────────────────────────┤
  │ Bun     │ Can replace Metro in some setups         │
  ├─────────┼──────────────────────────────────────────┤
  │ Re.Pack │ Webpack-based alternative bundler for RN │
  └─────────┴──────────────────────────────────────────┘

  ---
  Build Runners / Automation

  ┌──────────────────────┬───────────────────────────────────────────────┐
  │         Tool         │                     Notes                     │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ Fastlane             │ Most popular for signing, building, deploying │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ EAS CLI              │ Expo's managed build + submit system          │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ Expo CLI             │ expo run:android, expo run:ios                │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ React Native CLI     │ react-native run-android/ios                  │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ Turbo                │ Task runner for monorepos (turbo run build)   │
  ├──────────────────────┼───────────────────────────────────────────────┤
  │ Make / shell scripts │ Common in enterprise setups                   │
  └──────────────────────┴───────────────────────────────────────────────┘

  ---
  Key takeaway: Regardless of which layer is used, they all eventually call down to ./gradlew, xcodebuild, or a C++ compiler — so the three caches in activate react-native cover
  essentially every variation here.

  Sources:
  - https://addjam.com/blog/2025-03-18/react-native-apps-essential-tools-2025/
  - https://medium.com/@simplycodesmart/the-javascript-package-manager-showdown-npm-yarn-pnpm-and-bun-in-2025-076f659c743f
  - https://devblogs.sh/posts/best-cicd-tools-for-react-native
  - https://docs.fastlane.tools/getting-started/cross-platform/react-native/
