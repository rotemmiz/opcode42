package dev.opcode42.core.model

data class TextPartInput(val text: String)

data class FilePartInput(val mime: String, val url: String)  // url = "data:<mime>;base64,<b64>"
