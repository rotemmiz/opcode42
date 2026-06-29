package dev.opcode42.core.sdk

import dev.opcode42.core.model.Opcode42Json
import dev.opcode42.core.model.Project
import kotlinx.serialization.builtins.ListSerializer
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Decodes a real `GET /project` payload (the wire path `Opcode42Client.listProjects` uses) and
 * asserts the worktree + sandboxes are read, with unknown fields tolerated.
 */
class ProjectDecodeTest {

    @Test fun decodesWorktreeAndSandboxes_ignoringUnknownKeys() {
        val json = """
            [
              {"id":"global","worktree":"/","time":{"created":1,"updated":2},"sandboxes":[]},
              {"id":"abc","worktree":"/Users/x/git/returnzero","vcs":"git",
               "time":{"created":1,"updated":2},
               "sandboxes":["/Users/x/git/returnzero-1","/Users/x/git/returnzero_2"],
               "unknownField":123}
            ]
        """.trimIndent()

        val projects = Opcode42Json.decodeFromString(ListSerializer(Project.serializer()), json)

        assertEquals(2, projects.size)
        assertEquals("/", projects[0].worktree)
        assertEquals(emptyList<String>(), projects[0].sandboxes)
        assertEquals("git", projects[1].vcs)
        assertEquals(
            listOf("/Users/x/git/returnzero-1", "/Users/x/git/returnzero_2"),
            projects[1].sandboxes,
        )
    }
}
