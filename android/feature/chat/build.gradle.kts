plugins {
    id("opcode42.android.library")
    id("opcode42.android.compose")
    id("opcode42.android.hilt")
}

android {
    namespace = "dev.opcode42.feature.chat"
    testOptions {
        // Compose UI tests (QuestionCardTest, PermissionSheetTest) run under Robolectric on
        // the JVM; they need the merged resources + a working Android build to host the
        // Compose test rule.
        unitTests.isIncludeAndroidResources = true
    }
}

dependencies {
    api(project(":core:model"))
    api(project(":core:store"))
    implementation(project(":core:data"))
    implementation(project(":core:design"))
    implementation(project(":feature:connections"))
    implementation(libs.activity.compose)
    implementation(libs.android.lifecycle.viewmodel.compose)
    implementation(libs.hilt.navigation.compose)
    implementation(libs.compose.material.icons.extended)
    implementation(libs.datastore.preferences)
    implementation(libs.kotlinx.serialization.json)
    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation(libs.compose.ui.test.junit4)
    testImplementation(libs.robolectric)
    testDebugImplementation(libs.compose.ui.test.manifest)
    testDebugImplementation(libs.compose.ui.tooling)
}
