plugins {
    id("opcode42.android.library")
    id("opcode42.android.compose")
    id("opcode42.android.hilt")
}

android {
    namespace = "dev.opcode42.feature.settings"
}

dependencies {
    api(project(":core:model"))
    implementation(project(":core:data"))
    implementation(project(":feature:connections"))
    implementation(project(":feature:notifications"))
    implementation(libs.datastore.preferences)
    implementation(libs.android.lifecycle.viewmodel.compose)
    implementation(libs.hilt.navigation.compose)
    implementation(libs.compose.material.icons.extended)
}
