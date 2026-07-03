plugins {
    id("opcode42.android.library")
    id("opcode42.android.compose")
    id("opcode42.android.hilt")
}

android {
    namespace = "dev.opcode42.feature.terminal"
}

dependencies {
    implementation(project(":core:model"))
    implementation(project(":core:design"))
    implementation(project(":core:data"))
    implementation(project(":core:sdk"))
    implementation(project(":core:network"))
    implementation(project(":feature:connections"))
    implementation(libs.android.lifecycle.viewmodel.compose)
    implementation(libs.hilt.navigation.compose)
    implementation(libs.compose.material.icons.extended)
}
