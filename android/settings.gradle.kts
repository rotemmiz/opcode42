pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "forge-android"

include(":app")
include(":core:model")
include(":core:network")
include(":core:store")
include(":core:sdk")
include(":feature:connections")
include(":feature:sessions")
include(":feature:chat")
include(":feature:settings")
include(":feature:terminal")
include(":feature:notifications")
