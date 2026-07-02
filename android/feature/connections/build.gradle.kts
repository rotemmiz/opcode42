plugins {
    id("opcode42.android.library")
    id("opcode42.android.compose")
    id("opcode42.android.hilt")
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "dev.opcode42.feature.connections"
}

dependencies {
    api(project(":core:model"))
    api(project(":core:network"))
    api(project(":core:sdk"))
    implementation(libs.security.crypto)
    implementation(libs.android.lifecycle.viewmodel.compose)
    implementation(libs.hilt.navigation.compose)
    implementation(libs.compose.material.icons.extended)
}
