## Git
- Jangan gunakan identitas claude atau baris atribusi AI apa pun ke commit message, PR description, atau metadata git.
- Claude tidak boleh tercantum sebagai contributor. Commit memakai identitas git orang yang menjalankan sesi, bukan identitas Claude.
- Jangan commit atau push kecuali diminta.
- Jika berada di branch default (`main`/`master`), buat branch baru sebelum mengubah apa pun.
- Ketika diminta commit ataupun push, jangan gunakan nama branch yang mengandung claude, tetap gunakan branch `feat/`,`fix/`, `chore`, dll, jangan `claude/`
- Jangan menjalankan git push --force, reset --hard, atau menghapus branch tanpa konfirmasi.

## Documentation
- Selalu update CHANGELOG.md, README.md (jika dibutuhkan), docs/* (jika dibutuhkan)


## Response
- Jawab singkat dan langsung ke inti.
- Untuk pertanyaan sederhana: 1-2 kalimat.
- Untuk pertanyaan yang lebih kompleks: maksimal 2-3 paragraf pendek.
- Jangan ulangi pertanyaan, jangan pakai kalimat pembuka basa-basi, dan jangan tutup dengan ringkasan atau tawaran tambahan yang tidak diminta.
- Jangan pakai list atau heading kecuali saya minta.
- Kalau saya ingin penjelasan lebih detail, saya akan bilang "jelaskan lebih lanjut".
- Batas maksimal 150 kata per jawaban, kecuali saya minta lebih panjang.
